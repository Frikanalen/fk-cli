#!/usr/bin/env python3
"""Import recordings from a voctoweb instance (media.ccc.de) into Frikanalen.

voctoweb <https://github.com/voc/voctoweb> is the software behind media.ccc.de.
It holds a number of events (38C3, Camp, ...), each with a number of videos, and
each video was encoded in several formats:

    voctoweb-import.py events                 # 1. what is there
    voctoweb-import.py videos 38c3            # 2. what videos does it hold
    voctoweb-import.py import <video> -c ...  # 3. fetch the best recording
                                              #    and hand it to "fk video create"

Mind the vocabulary where it meets the API: voctoweb calls an event a
*conference*, and reserves *event* for the individual talk -- which is why a
video is addressed by what media.ccc.de calls its event GUID.

Only the standard library is used; the upload itself is delegated to the fk
binary, which must be on $PATH (or named with --fk) and logged in to the
environment you want to publish to ("fk env", "fk login"). A recording that
carries several video or audio tracks -- a camera mix and a slide capture, a
talk and its interpretations -- is remuxed down to one of each first, which
needs ffmpeg; see --no-normalize and --audio-language.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.parse

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import fkimport
from fkimport import Fatal, Item

DEFAULT_API = "https://api.media.ccc.de"

# Preference between containers of otherwise equal quality. mp4 wins: it is what
# Frikanalen's ingest is happiest with, and voctoweb publishes the same
# resolution in both.
CONTAINER_RANK = {"video/mp4": 2, "video/webm": 1}


# --------------------------------------------------------------------------
# voctoweb API
# --------------------------------------------------------------------------


class Voctoweb:
    """The read-only half of a voctoweb instance's public API."""

    def __init__(self, base: str = DEFAULT_API) -> None:
        self.base = base.rstrip("/")

    def _get(self, path: str) -> dict:
        return json.loads(fkimport.get(f"{self.base}{path}"))

    def conferences(self) -> list[dict]:
        return self._get("/public/conferences").get("conferences", [])

    def conference(self, ref: str) -> dict:
        """Look a conference up by acronym, or failing that by slug or title.

        The API only addresses conferences by acronym ("38c3"), but the slug
        ("congress/2024") is what the listing leads with, so accept either.
        """
        ref = _last_url_segment(ref)
        try:
            return self._get(f"/public/conferences/{urllib.parse.quote(ref)}")
        except KeyError:
            pass

        needle = ref.casefold()
        conferences = self.conferences()

        def fields(c: dict) -> list[str]:
            return [(c.get(k) or "").casefold() for k in ("acronym", "slug", "title")]

        # An exact hit on the acronym, slug or title settles it; only fall back
        # to substring matching when nothing lines up exactly, so that
        # "congress/2024" is not also read as "congress/2024-meta".
        matches = [c for c in conferences if needle in fields(c)]
        matches = matches or [
            c for c in conferences if any(needle in field for field in fields(c))
        ]
        if not matches:
            raise Fatal(f"no event matches {ref!r} (try \"events\" to list them)")
        if len(matches) > 1:
            names = ", ".join(sorted(m.get("acronym", "?") for m in matches))
            raise Fatal(f"{ref!r} matches several events: {names}")
        return self._get(f"/public/conferences/{urllib.parse.quote(matches[0]['acronym'])}")

    def event(self, ref: str) -> dict:
        """Look an event up by GUID or slug; a media.ccc.de URL works too."""
        ref = _last_url_segment(ref)
        try:
            return self._get(f"/public/events/{urllib.parse.quote(ref)}")
        except KeyError:
            raise Fatal(f"no event with GUID or slug {ref!r}") from None


def _last_url_segment(ref: str) -> str:
    """Reduce a pasted URL to the identifier at its end, leaving plain refs be."""
    if not ref.startswith(("http://", "https://")):
        return ref
    return urllib.parse.urlparse(ref).path.rstrip("/").rsplit("/", 1)[-1]


# --------------------------------------------------------------------------
# Choosing what to download
# --------------------------------------------------------------------------


def _languages(recording: dict) -> list[str]:
    # A recording is tagged "deu", or "deu-eng" when it carries a translation.
    return [part for part in str(recording.get("language") or "").split("-") if part]


def _quality(recording: dict) -> tuple:
    pixels = int(recording.get("width") or 0) * int(recording.get("height") or 0)
    return (
        1 if recording.get("high_quality") else 0,
        pixels,
        CONTAINER_RANK.get(recording.get("mime_type"), 0),
        int(recording.get("size") or 0),
    )


def pick_recording(event: dict, language: str | None = None,
                   container: str | None = None) -> dict:
    """Return the highest-quality video recording of an event.

    Quality is voctoweb's own high_quality flag first, then resolution, then
    mp4 over webm, with the file size breaking any remaining tie.
    """
    candidates = [r for r in event.get("recordings", [])
                  if str(r.get("mime_type", "")).startswith("video/")]
    if not candidates:
        raise Fatal(f"{event.get('guid')}: the event has no video recording")

    if container:
        wanted = f"video/{container}"
        narrowed = [r for r in candidates if r.get("mime_type") == wanted]
        if not narrowed:
            have = ", ".join(sorted({str(r.get("mime_type")) for r in candidates}))
            raise Fatal(f"no {container} recording for this event (have: {have})")
        candidates = narrowed

    if language:
        narrowed = [r for r in candidates if language in _languages(r)]
        if not narrowed:
            have = ", ".join(sorted({str(r.get("language")) for r in candidates}))
            raise Fatal(f"no recording in {language!r} for this event (have: {have})")
        candidates = narrowed
    elif event.get("original_language"):
        # Prefer the language the talk was held in, but do not insist: some
        # events are only published with a translation attached.
        narrowed = [r for r in candidates
                    if event["original_language"] in _languages(r)]
        candidates = narrowed or candidates

    return max(candidates, key=_quality)


# --------------------------------------------------------------------------
# Turning an event into something fk can publish
# --------------------------------------------------------------------------


def compose_title(event: dict) -> str:
    title = (event.get("title") or "").strip()
    if subtitle := (event.get("subtitle") or "").strip():
        title = f"{title} - {subtitle}"
    return fkimport.truncate_title(title)


def compose_description(event: dict) -> str:
    """Describe the talk, and credit where the recording came from."""
    parts = []
    if description := (event.get("description") or "").strip():
        parts.append(description)

    if persons := [p for p in event.get("persons") or [] if p]:
        parts.append("Med: " + ", ".join(persons))

    conference = event.get("conference_title") or ""
    date = (event.get("date") or "")[:10]
    origin = " ".join(x for x in ("Opptak fra", conference, date and f"({date})") if x)
    if link := event.get("frontend_link") or event.get("url"):
        origin = f"{origin.rstrip()}: {link}" if conference else link
    if origin:
        parts.append(origin)

    return "\n\n".join(parts)


def to_item(event: dict, ref: str, recording: dict) -> Item:
    # A recording tagged "eng-deu-fra" carries the talk plus its
    # interpretations; the one to keep is the language it was held in.
    languages = _languages(recording)
    spoken = event.get("original_language") or ""
    if spoken not in languages:
        spoken = languages[0] if languages else ""

    return Item(
        ref=ref,
        title=compose_title(event),
        description=compose_description(event),
        url=recording["recording_url"],
        filename=os.path.basename(recording["filename"]),
        note=f"{recording.get('folder')} "
             f"{recording.get('width')}x{recording.get('height')} "
             f"{recording.get('language')} ~{recording.get('size')} MB",
        language=spoken,
    )


# --------------------------------------------------------------------------
# Commands
# --------------------------------------------------------------------------


def cmd_events(args, api: Voctoweb) -> None:
    conferences = api.conferences()
    if args.pattern:
        needle = args.pattern.casefold()
        conferences = [
            c for c in conferences
            if needle in (c.get("acronym") or "").casefold()
            or needle in (c.get("title") or "").casefold()
            or needle in (c.get("slug") or "").casefold()
        ]
    conferences.sort(key=lambda c: c.get("event_last_released_at") or "", reverse=True)

    if args.json:
        json.dump(conferences, sys.stdout, indent=2)
        print()
        return

    fkimport.print_table(
        ["acronym", "released", "title"],
        [[c.get("acronym") or "", (c.get("event_last_released_at") or "")[:10],
          c.get("title") or ""] for c in conferences],
    )


def cmd_videos(args, api: Voctoweb) -> None:
    conference = api.conference(args.event)
    events = conference.get("events", [])
    if args.search:
        needle = args.search.casefold()
        events = [
            e for e in events
            if needle in (e.get("title") or "").casefold()
            or needle in (e.get("subtitle") or "").casefold()
            or needle in " ".join(e.get("persons") or []).casefold()
        ]
    events.sort(key=lambda e: e.get("date") or "")

    if args.json:
        json.dump(events, sys.stdout, indent=2)
        print()
        return

    print(f"{conference.get('title')} ({conference.get('acronym')}) "
          f"- {len(events)} videos", file=sys.stderr)
    fkimport.print_table(
        ["guid", "date", "length", "lang", "title"],
        [[e.get("guid") or "", (e.get("date") or "")[:10],
          fkimport.duration(e.get("length")), e.get("original_language") or "",
          compose_title(e)] for e in events],
    )


def cmd_import(args, api: Voctoweb) -> None:
    def resolve(ref: str) -> Item:
        event = api.event(ref)
        recording = pick_recording(event, language=args.language,
                                   container=args.container)
        return to_item(event, ref, recording)

    fkimport.import_items(args.video, resolve, args)


# --------------------------------------------------------------------------
# Entry point
# --------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="voctoweb-import.py",
        description="List the events and videos on a voctoweb instance "
                    "(media.ccc.de), and publish a video's best recording to "
                    "Frikanalen via fk.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="examples:\n"
               "  voctoweb-import.py events congress\n"
               "  voctoweb-import.py videos 38c3 --search rekordbox\n"
               "  voctoweb-import.py import 98C007E2-A3B2-44FD-ADF5-D21224DE0988 "
               "-c Kultur\n",
    )
    parser.add_argument("--api", default=os.environ.get("VOCTOWEB_API", DEFAULT_API),
                        help=f"voctoweb API base URL (default: {DEFAULT_API})")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("events",
                       help="list the events the voctoweb instance holds")
    p.add_argument("pattern", nargs="?",
                   help="only events whose acronym, slug or title contains this")
    p.add_argument("--json", action="store_true", help="dump the raw API objects")
    p.set_defaults(func=cmd_events)

    p = sub.add_parser("videos",
                       help="list the videos of one event")
    p.add_argument("event", help="event acronym, slug or URL, e.g. 38c3")
    p.add_argument("--search", help="only videos matching this title, subtitle or speaker")
    p.add_argument("--json", action="store_true", help="dump the raw API objects")
    p.set_defaults(func=cmd_videos)

    p = sub.add_parser("import",
                       help="download a video at its best quality and upload it to Frikanalen")
    p.add_argument("video", nargs="+",
                   help="the talk's GUID, slug or media.ccc.de URL (repeatable)")
    p.add_argument("--language", metavar="ISO639-3",
                   help="pick the recording in this language, e.g. eng "
                        "(default: the talk's original language)")
    p.add_argument("--container", choices=["mp4", "webm"],
                   help="restrict to one container (default: whichever is better)")
    fkimport.add_import_arguments(p)
    p.set_defaults(func=cmd_import)

    return parser


def main(argv: list[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    args.func(args, Voctoweb(args.api))


if __name__ == "__main__":
    sys.exit(fkimport.run(main))
