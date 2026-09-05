#!/usr/bin/env python3
"""Import recordings from the DEF CON media archive into Frikanalen.

The archive at <https://media.defcon.org/> is a set of browsable directory
listings rather than an API.  This script reads those listings and presents
the same interface as the other contrib importers:

    defcon-import.py events                   # 1. which DEF CONs are there
    defcon-import.py videos dc33              # 2. list one event's recordings
    defcon-import.py import dc33#1 -c Kultur  # 3. download and publish one

By default, ``videos`` follows the archive branches whose names contain
"video", "village", or "creator", as well as video files placed directly in
the event directory.  That finds the conference talks without walking photo,
music, badge, and software collections.  ``--all-directories`` performs an
exhaustive walk when looking for less conventional recordings.

Only the standard library is used.  Uploading is delegated to the fk binary,
which must be on $PATH (or named with --fk) and logged in to the environment
you want to publish to ("fk env", "fk login").
"""

from __future__ import annotations

import argparse
from html.parser import HTMLParser
import json
import os
import re
import sys
import urllib.parse

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import fkimport
from fkimport import Fatal, Item

DEFAULT_BASE = "https://media.defcon.org/"
VIDEO_EXTENSIONS = {".m4v", ".mkv", ".mov", ".mp4", ".webm"}
RECORDING_DIRECTORY_WORDS = ("video", "village", "creator")


# --------------------------------------------------------------------------
# Reading nginx fancy-index pages
# --------------------------------------------------------------------------


class DirectoryEntry:
    def __init__(self, name: str, url: str, is_dir: bool,
                 size: str = "", modified: str = "") -> None:
        self.name = name.rstrip("/")
        self.url = url
        self.is_dir = is_dir
        self.size = size
        self.modified = modified


class _ListingParser(HTMLParser):
    """Extract rows from the archive's ``<table id="list">`` listing."""

    def __init__(self, base: str) -> None:
        super().__init__(convert_charrefs=True)
        self.base = base
        self.in_list = False
        self.found_list = False
        self.row = None
        self.cell = ""
        self.cell_text: list[str] = []
        self.entries: list[DirectoryEntry] = []

    def handle_starttag(self, tag: str, attrs) -> None:
        attributes = dict(attrs)
        if tag == "table" and attributes.get("id") == "list":
            self.in_list = True
            self.found_list = True
        elif self.in_list and tag == "tr":
            self.row = {"href": "", "name": "", "size": "", "date": ""}
        elif self.row is not None and tag == "td":
            self.cell = attributes.get("class", "")
            self.cell_text = []
        elif self.row is not None and tag == "a" and self.cell == "link":
            self.row["href"] = attributes.get("href", "")
            self.row["name"] = attributes.get("title", "")

    def handle_data(self, data: str) -> None:
        if self.row is not None and self.cell:
            self.cell_text.append(data)

    def handle_endtag(self, tag: str) -> None:
        if tag == "td" and self.row is not None:
            value = "".join(self.cell_text).strip()
            if self.cell == "link" and not self.row["name"]:
                self.row["name"] = value.rstrip("/")
            elif self.cell == "size":
                self.row["size"] = value
            elif self.cell == "date":
                self.row["date"] = value
            self.cell = ""
            self.cell_text = []
        elif tag == "tr" and self.row is not None:
            href = self.row["href"]
            if href and href not in ("../", "./") and not href.startswith("?"):
                self.entries.append(DirectoryEntry(
                    self.row["name"], urllib.parse.urljoin(self.base, href),
                    urllib.parse.urlsplit(href).path.endswith("/"),
                    self.row["size"], self.row["date"]))
            self.row = None
        elif tag == "table" and self.in_list:
            self.in_list = False


def _parse_listing(document: bytes | str, base: str) -> _ListingParser:
    parser = _ListingParser(base)
    if isinstance(document, bytes):
        document = document.decode("utf-8", errors="replace")
    parser.feed(document)
    return parser


def parse_listing(document: bytes | str, base: str) -> list[DirectoryEntry]:
    return _parse_listing(document, base).entries


def _is_video(name: str) -> bool:
    return os.path.splitext(name)[1].casefold() in VIDEO_EXTENSIONS


def _slug(name: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", name.casefold()).strip("-")


# --------------------------------------------------------------------------
# Archive objects
# --------------------------------------------------------------------------


class Conference:
    def __init__(self, name: str, url: str, modified: str = "") -> None:
        self.name = name
        self.url = url
        self.modified = modified
        match = re.fullmatch(r"DEF CON (\d+)", name, re.IGNORECASE)
        self.number = int(match.group(1)) if match else 0
        self.ref = f"dc{self.number}" if self.number else _slug(name)

    @property
    def year(self) -> str:
        # DEF CON 1 was held in 1993, and the annual numbering has remained
        # contiguous since then.
        return str(1992 + self.number) if self.number else ""


class Video:
    def __init__(self, conference: Conference, entry: DirectoryEntry,
                 number: int) -> None:
        self.conference = conference
        self.name = entry.name
        self.url = entry.url
        self.size = entry.size
        self.modified = entry.modified
        self.number = number
        event_path = urllib.parse.unquote(urllib.parse.urlsplit(conference.url).path)
        path = urllib.parse.unquote(urllib.parse.urlsplit(self.url).path)
        self.path = path[len(event_path):].lstrip("/")
        self.section = self._section()
        self.title, self.speakers = self._metadata()

    @property
    def ref(self) -> str:
        return f"{self.conference.ref}#{self.number}"

    def _section(self) -> str:
        if "/" not in self.path:
            return ""
        section = self.path.split("/", 1)[0]
        section = re.sub(rf"^{re.escape(self.conference.name)}\s+", "", section,
                         flags=re.IGNORECASE)
        if section.casefold() in ("video", "videos", "video and slides"):
            return ""
        return section

    def _metadata(self) -> tuple[str, str]:
        stem = os.path.splitext(self.name)[0].strip()
        stem = re.sub(rf"^{re.escape(self.conference.name)}\s*-\s*", "", stem,
                      flags=re.IGNORECASE)
        parts = [part.strip() for part in stem.split(" - ") if part.strip()]
        if len(parts) < 2:
            return stem, ""

        # The archive used "speaker - title" through DC30 and changed to
        # "title - speaker" for DC31.  Village recordings use
        # "village - speaker - title" regardless of the main-stage order.
        if self.section and any(word in self.section.casefold()
                                for word in ("village", "creator")) \
                and len(parts) >= 3:
            return " - ".join(parts[2:]), parts[1]
        if self.conference.number and self.conference.number <= 30:
            return " - ".join(parts[1:]), parts[0]
        return " - ".join(parts[:-1]), parts[-1]


class Archive:
    def __init__(self, base: str = DEFAULT_BASE) -> None:
        self.base = base.rstrip("/") + "/"
        self._listings: dict[str, list[DirectoryEntry]] = {}
        self._conferences: list[Conference] | None = None
        self._videos: dict[tuple[str, bool], list[Video]] = {}

    def listing(self, url: str) -> list[DirectoryEntry]:
        if url not in self._listings:
            try:
                document = fkimport.get(url, accept="text/html")
            except KeyError:
                raise Fatal(f"no directory at {url}") from None
            parser = _parse_listing(document, url)
            if not parser.found_list:
                raise Fatal(f"{url}: response was not an archive directory listing")
            # A listing containing only its parent link is a valid empty
            # directory.  The archive has a few stale caption directories of
            # exactly that kind, and they should not abort a larger walk.
            self._listings[url] = parser.entries
        return self._listings[url]

    def conferences(self) -> list[Conference]:
        if self._conferences is None:
            conferences = []
            for entry in self.listing(self.base):
                if not entry.is_dir or not self._looks_like_conference(entry.name):
                    continue
                conferences.append(Conference(entry.name, entry.url, entry.modified))
            conferences.sort(key=lambda c: (c.number > 0, c.number, c.name),
                             reverse=True)
            self._conferences = conferences
        return self._conferences

    @staticmethod
    def _looks_like_conference(name: str) -> bool:
        if re.fullmatch(r"DEF CON \d+", name, re.IGNORECASE):
            return True
        return bool(re.fullmatch(r"DEF CON (?:Bahrain|China|SG|NYE)\b.*", name,
                                 re.IGNORECASE))

    def conference(self, ref: str) -> Conference:
        ref = ref.strip().rstrip("/")
        if ref.startswith(("http://", "https://")):
            path = urllib.parse.unquote(urllib.parse.urlsplit(ref).path).strip("/")
            ref = path.split("/", 1)[0]
        needle = ref.casefold().replace(" ", "")

        def aliases(conference: Conference) -> set[str]:
            values = {conference.name, conference.ref, _slug(conference.name)}
            if conference.number:
                values.update({str(conference.number), f"defcon{conference.number}"})
            return {value.casefold().replace(" ", "") for value in values}

        matches = [c for c in self.conferences() if needle in aliases(c)]
        if not matches:
            plain = ref.casefold()
            matches = [c for c in self.conferences()
                       if plain in c.name.casefold() or plain in c.ref.casefold()]
        if not matches:
            raise Fatal(f"no event matches {ref!r} (try \"events\" to list them)")
        if len(matches) > 1:
            names = ", ".join(c.ref for c in matches[:10])
            raise Fatal(f"{ref!r} matches several events: {names}")
        return matches[0]

    def videos(self, conference: Conference,
               all_directories: bool = False) -> list[Video]:
        key = (conference.url, all_directories)
        if key in self._videos:
            return self._videos[key]

        files = []
        root_entries = self.listing(conference.url)
        files.extend(e for e in root_entries if not e.is_dir and _is_video(e.name))

        if all_directories:
            pending = [e for e in root_entries if e.is_dir]
        else:
            pending = [e for e in root_entries if e.is_dir and any(
                word in e.name.casefold() for word in RECORDING_DIRECTORY_WORDS)]

        seen = {conference.url}
        while pending:
            directory = pending.pop()
            if directory.url in seen:
                continue
            seen.add(directory.url)
            for entry in self.listing(directory.url):
                if entry.is_dir:
                    pending.append(entry)
                elif _is_video(entry.name):
                    files.append(entry)

        files.sort(key=lambda e: urllib.parse.unquote(
            urllib.parse.urlsplit(e.url).path).casefold())
        videos = [Video(conference, entry, number)
                  for number, entry in enumerate(files, 1)]
        self._videos[key] = videos
        return videos

    def video(self, ref: str, scope: Conference | None = None,
              all_directories: bool = False) -> Video:
        ref = ref.strip()
        if "#" in ref:
            conference_ref, _, number = ref.rpartition("#")
            conference = self.conference(conference_ref) if conference_ref else scope
            if conference is None:
                raise Fatal(f"{ref!r}: name the event before the '#'")
            if not number.isdigit():
                raise Fatal(f"{ref!r}: expected a number after the '#'")
            videos = self.videos(conference, all_directories)
            index = int(number)
            if index < 1 or index > len(videos):
                raise Fatal(f"{conference.ref} has no video #{number} "
                            f"(it has {len(videos)})")
            return videos[index - 1]

        if ref.startswith(("http://", "https://")):
            conference = scope or self.conference(ref)
            pool = self.videos(conference, all_directories)
            matches = [video for video in pool if video.url == ref]
        else:
            if scope is None:
                raise Fatal("name the event with --event, or use a ref from "
                            "\"videos\" such as dc33#1")
            pool = self.videos(scope, all_directories)
            needle = ref.casefold()
            for test in (lambda v: v.name.casefold() == needle,
                         lambda v: v.title.casefold() == needle,
                         lambda v: needle in v.name.casefold()
                         or needle in v.title.casefold()):
                matches = [video for video in pool if test(video)]
                if matches:
                    break

        if not matches:
            raise Fatal(f"nothing in the archive matches {ref!r}")
        if len(matches) > 1:
            listed = "\n  ".join(f"{video.ref}  {video.title}"
                                  for video in matches[:8])
            more = "" if len(matches) <= 8 else f"\n  ... and {len(matches) - 8} more"
            raise Fatal(f"{ref!r} matches several videos, pick one:\n  "
                        f"{listed}{more}")
        return matches[0]


# --------------------------------------------------------------------------
# Turning a recording into something fk can publish
# --------------------------------------------------------------------------


def compose_description(video: Video) -> str:
    parts = []
    if video.speakers:
        parts.append("Med: " + video.speakers)
    if video.section:
        parts.append("Seksjon: " + video.section)
    when = f" ({video.conference.year})" if video.conference.year else ""
    parts.append(f"Opptak fra {video.conference.name}{when}: {video.url}")
    return "\n\n".join(parts)


def to_item(video: Video, ref: str) -> Item:
    note = " / ".join(x for x in (video.section, video.size) if x)
    return Item(
        ref=ref,
        title=video.title,
        description=compose_description(video),
        url=video.url,
        filename=video.name,
        note=note,
    )


# --------------------------------------------------------------------------
# Commands
# --------------------------------------------------------------------------


def cmd_events(args, archive: Archive) -> None:
    conferences = archive.conferences()
    if args.pattern:
        needle = args.pattern.casefold()
        conferences = [c for c in conferences
                       if needle in c.ref.casefold()
                       or needle in c.name.casefold()
                       or needle in c.year]
    if args.json:
        json.dump([{"ref": c.ref, "year": c.year, "title": c.name,
                    "url": c.url, "modified": c.modified}
                   for c in conferences], sys.stdout, indent=2)
        print()
        return
    fkimport.print_table(
        ["event", "year", "title"],
        [[c.ref, c.year, c.name] for c in conferences],
    )


def cmd_videos(args, archive: Archive) -> None:
    conference = archive.conference(args.event)
    videos = archive.videos(conference, args.all_directories)
    if args.search:
        needle = args.search.casefold()
        videos = [video for video in videos
                  if needle in video.title.casefold()
                  or needle in video.speakers.casefold()
                  or needle in video.section.casefold()]
    if args.json:
        json.dump([{"ref": v.ref, "title": v.title, "speakers": v.speakers,
                    "section": v.section, "size": v.size, "url": v.url,
                    "filename": v.name}
                   for v in videos], sys.stdout, indent=2)
        print()
        return
    print(f"{conference.name} - {len(videos)} videos "
          f"(refs below work with \"import\")", file=sys.stderr)
    fkimport.print_table(
        ["ref", "size", "section", "title", "speakers"],
        [[v.ref, v.size, v.section, v.title, v.speakers] for v in videos],
    )


def cmd_import(args, archive: Archive) -> None:
    scope = archive.conference(args.event) if args.event else None

    def resolve(ref: str) -> Item:
        return to_item(archive.video(ref, scope, args.all_directories), ref)

    fkimport.import_items(args.video, resolve, args)


# --------------------------------------------------------------------------
# Entry point
# --------------------------------------------------------------------------


def _add_walk_argument(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--all-directories", action="store_true",
                        help="also search non-recording branches such as CTF, "
                             "contests, workshops, and music")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="defcon-import.py",
        description="List recordings on media.defcon.org and publish one to "
                    "Frikanalen via fk.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="examples:\n"
               "  defcon-import.py events\n"
               "  defcon-import.py videos dc33 --search phrack\n"
               "  defcon-import.py import dc33#1 -c Kultur\n",
    )
    parser.add_argument("--base",
                        default=os.environ.get("DEFCON_MEDIA", DEFAULT_BASE),
                        help=f"archive base URL (default: {DEFAULT_BASE})")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("events", help="list the DEF CON events in the archive")
    p.add_argument("pattern", nargs="?",
                   help="only events whose ref, title, or year contains this")
    p.add_argument("--json", action="store_true", help="dump structured results")
    p.set_defaults(func=cmd_events)

    p = sub.add_parser("videos", help="list the recordings of one event")
    p.add_argument("event", help="event number, ref, name, or URL, e.g. 33 or dc33")
    p.add_argument("--search", help="only videos matching title, speaker, or section")
    p.add_argument("--json", action="store_true", help="dump structured results")
    _add_walk_argument(p)
    p.set_defaults(func=cmd_videos)

    p = sub.add_parser("import",
                       help="download a recording and upload it to Frikanalen")
    p.add_argument("video", nargs="+",
                   help="a ref from \"videos\" (dc33#1), archive URL, file name, "
                        "or title (repeatable)")
    p.add_argument("--event",
                   help="resolve file names and titles within this event")
    _add_walk_argument(p)
    fkimport.add_import_arguments(p)
    p.set_defaults(func=cmd_import)
    return parser


def main(argv: list[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    args.func(args, Archive(args.base))


if __name__ == "__main__":
    sys.exit(fkimport.run(main))
