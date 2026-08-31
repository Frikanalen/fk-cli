#!/usr/bin/env python3
"""Import recordings from the Debian meetings archive into Frikanalen.

The Debian video team keeps an inventory of everything in the archive at
<https://salsa.debian.org/debconf-video-team/archive-meta>: one YAML file per
event, listing each video with its title, speakers, abstract, schedule slot, and
the file names of every format it was encoded in. That inventory, rather than
the bare directory listing at meetings-archive.debian.net, is what this script
reads -- it is the only place the real titles and descriptions live.

    debconf-import.py events                    # 1. what is in the archive
    debconf-import.py videos debconf25          # 2. what videos does it hold
    debconf-import.py import debconf25#1 -c ... # 3. fetch the master recording
                                                #    and hand it to "fk video create"

The inventory is fetched once and cached, so only the first run of the day
touches Salsa. Uploading is delegated to the fk binary, which must be on $PATH
(or named with --fk) and logged in to the environment you want to publish to
("fk env", "fk login").

Needs PyYAML: "apt install python3-yaml", or "pip install pyyaml".
"""

from __future__ import annotations

import argparse
import os
import re
import sys
import tarfile
import time
import urllib.parse

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import fkimport
from fkimport import Fatal, Item

try:
    import yaml
except ImportError:  # pragma: no cover - depends on the machine, not the code
    sys.exit("error: PyYAML is required: apt install python3-yaml, "
             "or pip install pyyaml")

# The inventory is a git repository; this fetches just its metadata/ subtree.
ARCHIVE_META = ("https://salsa.debian.org/debconf-video-team/archive-meta"
                "/-/archive/main/archive-meta-main.tar.gz?path=metadata")

CACHE_MAX_AGE = 24 * 3600  # a day; the inventory changes a few times a year

def cache_path() -> str:
    base = os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache")
    return os.path.join(base, "fk-cli", "debian-archive-meta.tar.gz")


# --------------------------------------------------------------------------
# The inventory
# --------------------------------------------------------------------------


class Conference:
    """One YAML file from the inventory: an event and its videos."""

    def __init__(self, path: str, document: dict) -> None:
        # "metadata/2023/hamburg-reunion.yml" -> "2023/hamburg-reunion". The
        # year has to stay in: several meetings recur under the same name.
        self.ref = re.sub(r"^metadata/|\.yml$", "", path)
        self.slug = self.ref.split("/")[-1]
        self.year = self.ref.split("/")[0]

        meta = document.get("conference") or {}
        self.title = meta.get("title") or self.slug
        self.series = meta.get("series") or ""
        self.location = meta.get("location") or ""
        self.website = meta.get("website") or ""
        self.dates = [str(d) for d in (meta.get("date") or [])]
        self.video_base = meta.get("video_base") or ""
        self.formats = meta.get("video_formats") or {}
        self.videos = [Video(self, i, v)
                       for i, v in enumerate(document.get("videos") or [], 1)]

    @property
    def date_range(self) -> str:
        if not self.dates:
            return ""
        if len(self.dates) > 1 and self.dates[0] != self.dates[-1]:
            return f"{self.dates[0]}..{self.dates[-1]}"
        return self.dates[0]

    def is_video_format(self, name: str) -> bool:
        """Audio-only encodings are listed alongside the video ones."""
        spec = self.formats.get(name) or {}
        if spec.get("resolution") or spec.get("vcodec"):
            return True
        # Unknown formats are assumed to be video; only an explicitly
        # audio-only encoding (an acodec and nothing else) is ruled out.
        return not spec.get("acodec")

    def pixels(self, name: str) -> int:
        resolution = str((self.formats.get(name) or {}).get("resolution") or "")
        match = re.match(r"(\d+)\s*x\s*(\d+)", resolution)
        return int(match.group(1)) * int(match.group(2)) if match else 0

    def describe_format(self, name: str) -> str:
        spec = self.formats.get(name) or {}
        bits = [name]
        for key in ("resolution", "vcodec", "acodec", "bitrate"):
            if spec.get(key):
                bits.append(str(spec[key]))
        return " ".join(bits)


class Video:
    """One talk, with the file names of every format it was encoded in."""

    def __init__(self, conference: Conference, number: int, entry: dict) -> None:
        self.conference = conference
        self.number = number
        self.title = (entry.get("title") or "").strip()
        self.speakers = [s for s in (entry.get("speakers") or []) if s]
        self.description = (entry.get("description") or "").strip()
        self.details_url = entry.get("details_url") or ""
        self.room = entry.get("room") or ""
        self.start = entry.get("start")
        self.end = entry.get("end")
        self.language = entry.get("language") or ""
        # A "non-free" note means the video team believes the recording is not
        # redistributable as-is -- worth knowing before broadcasting it.
        self.non_free = entry.get("non-free") or ""

        # "default" is the master, per the inventory's own schema.
        self.files = {"default": entry["video"]}
        self.files.update(entry.get("alt_formats") or {})

    @property
    def ref(self) -> str:
        return f"{self.conference.ref}#{self.number}"

    @property
    def stem(self) -> str:
        return os.path.basename(self.files["default"]).split(".")[0]

    @property
    def seconds(self) -> int:
        if not (self.start and self.end):
            return 0
        try:
            return max(int((self.end - self.start).total_seconds()), 0)
        except TypeError:  # one of them is a bare date, or a string
            return 0

    def url(self, fmt: str) -> str:
        base = self.conference.video_base
        if not base:
            raise Fatal(f"{self.conference.ref}: the inventory gives no video_base")
        return urllib.parse.urljoin(base, urllib.parse.quote(self.files[fmt]))

    def best_format(self, wanted: str | None = None) -> str:
        """Pick the highest-quality encoding: resolution first, then container,
        with the master breaking ties."""
        if wanted:
            if wanted not in self.files:
                have = ", ".join(sorted(self.files))
                raise Fatal(f"no {wanted!r} encoding of this video (have: {have})")
            return wanted

        candidates = [f for f in self.files if self.conference.is_video_format(f)]
        if not candidates:
            raise Fatal("the inventory lists no video encoding of this talk")
        # Resolution decides, and the inventory's schema calls "default" the
        # master, so it takes any tie -- which today is every one of them.
        return max(candidates, key=lambda f: (
            self.conference.pixels(f), 1 if f == "default" else 0, f))


class Inventory:
    """Every conference in the archive, from a cached copy of the metadata."""

    def __init__(self, refresh: bool = False, cache: str | None = None) -> None:
        self.path = cache or cache_path()
        self._fetch(refresh)
        try:
            self.conferences = self._load()
        except tarfile.TarError as err:
            if refresh:
                raise Fatal(f"{self.path}: not a readable archive ({err})") from None
            # A half-written or truncated cache is not worth diagnosing:
            # throw it away and fetch it again.
            print(f"warning: the cached inventory is unreadable ({err}); "
                  f"fetching it again", file=sys.stderr)
            os.remove(self.path)
            self._fetch(refresh=True)
            self.conferences = self._load()

    def _fetch(self, refresh: bool) -> None:
        fresh = (os.path.exists(self.path)
                 and time.time() - os.path.getmtime(self.path) < CACHE_MAX_AGE)
        if fresh and not refresh:
            return

        try:
            blob = fkimport.get(ARCHIVE_META, accept="application/gzip")
        except (Fatal, KeyError) as err:
            if os.path.exists(self.path):
                # A stale inventory beats no inventory: Salsa being down should
                # not stop an import of something listed months ago.
                print(f"warning: could not refresh the inventory ({err}); "
                      f"using the cached copy", file=sys.stderr)
                return
            raise Fatal(f"could not fetch the archive inventory: {err}") from None

        os.makedirs(os.path.dirname(self.path), exist_ok=True)
        with open(self.path + ".part", "wb") as fh:
            fh.write(blob)
        os.replace(self.path + ".part", self.path)

    def _load(self) -> list[Conference]:
        conferences = []
        with tarfile.open(self.path) as tar:
            for member in tar.getmembers():
                # Entries are "archive-meta-main-metadata/metadata/2025/x.yml".
                name = member.name.split("/", 1)[-1]
                if not name.endswith(".yml") or name.endswith("index.yml"):
                    continue
                handle = tar.extractfile(member)
                if handle is None:
                    continue
                document = yaml.safe_load(handle.read()) or {}
                if document.get("conference"):
                    conferences.append(Conference(name, document))
        if not conferences:
            raise Fatal(f"{self.path}: no event metadata in the cached "
                        f"inventory (delete it and retry)")
        conferences.sort(key=lambda c: (c.year, c.slug), reverse=True)
        return conferences

    def conference(self, ref: str) -> Conference:
        """Resolve an event: "2025/debconf25", "debconf25", or a fragment."""
        needle = ref.strip("/").casefold()
        for test in (lambda c: c.ref.casefold() == needle,
                     lambda c: c.slug.casefold() == needle,
                     lambda c: c.title.casefold() == needle,
                     lambda c: needle in c.ref.casefold()
                     or needle in c.title.casefold()):
            matches = [c for c in self.conferences if test(c)]
            if len(matches) == 1:
                return matches[0]
            if len(matches) > 1:
                names = ", ".join(c.ref for c in matches[:8])
                raise Fatal(f"{ref!r} matches several events: {names}")
        raise Fatal(f"no event matches {ref!r} "
                    f"(try \"events\" to list them)")

    def video(self, ref: str, scope: Conference | None = None) -> Video:
        """Resolve a video: "<event>#<n>", a file name, a URL, or a title."""
        ref = ref.strip()

        if "#" in ref:
            conference_ref, _, number = ref.rpartition("#")
            conference = self.conference(conference_ref) if conference_ref else scope
            if conference is None:
                raise Fatal(f"{ref!r}: name the event before the '#'")
            if not number.isdigit():
                raise Fatal(f"{ref!r}: expected a number after the '#'")
            for video in conference.videos:
                if video.number == int(number):
                    return video
            raise Fatal(f"{conference.ref} has no video #{number} "
                        f"(it has {len(conference.videos)})")

        pool = scope.videos if scope else [v for c in self.conferences
                                           for v in c.videos]
        if ref.startswith(("http://", "https://")):
            matches = [v for v in pool
                       if any(v.url(f) == ref for f in v.files)]
        else:
            needle = ref.casefold()
            for test in (lambda v: v.stem.casefold() == needle,
                         lambda v: v.title.casefold() == needle,
                         lambda v: needle in v.stem.casefold()
                         or needle in v.title.casefold()):
                matches = [v for v in pool if test(v)]
                if matches:
                    break

        if not matches:
            raise Fatal(f"nothing in the archive matches {ref!r}")
        if len(matches) > 1:
            listed = "\n  ".join(f"{v.ref}  {v.title}" for v in matches[:8])
            more = "" if len(matches) <= 8 else f"\n  ... and {len(matches) - 8} more"
            raise Fatal(f"{ref!r} matches several videos, "
                        f"pick one:\n  {listed}{more}")
        return matches[0]


# --------------------------------------------------------------------------
# Turning a talk into something fk can publish
# --------------------------------------------------------------------------


def compose_description(video: Video) -> str:
    parts = []
    if video.description:
        parts.append(video.description)
    if video.speakers:
        parts.append("Med: " + ", ".join(video.speakers))

    conference = video.conference
    where = ", ".join(x for x in (conference.title, conference.location) if x)
    when = conference.date_range
    origin = f"Opptak fra {where}".rstrip()
    if when:
        origin += f" ({when})"
    if link := video.details_url or conference.website:
        origin += f": {link}"
    parts.append(origin)

    return "\n\n".join(parts)


def to_item(video: Video, ref: str, fmt: str | None) -> Item:
    chosen = video.best_format(fmt)
    return Item(
        ref=ref,
        title=video.title,
        description=compose_description(video),
        url=video.url(chosen),
        filename=os.path.basename(video.files[chosen]),
        note=f"{video.conference.describe_format(chosen)}"
             + (f" [{video.language}]" if video.language else ""),
        warning=video.non_free,
    )


# --------------------------------------------------------------------------
# Commands
# --------------------------------------------------------------------------


def cmd_events(args, inventory: Inventory) -> None:
    conferences = inventory.conferences
    if args.pattern:
        needle = args.pattern.casefold()
        conferences = [c for c in conferences
                       if needle in c.ref.casefold()
                       or needle in c.title.casefold()
                       or needle in c.location.casefold()]
    fkimport.print_table(
        ["event", "dates", "videos", "title"],
        [[c.ref, c.date_range, str(len(c.videos)),
          ", ".join(x for x in (c.title, c.location) if x)]
         for c in conferences],
    )


def cmd_videos(args, inventory: Inventory) -> None:
    conference = inventory.conference(args.event)
    videos = conference.videos
    if args.search:
        needle = args.search.casefold()
        videos = [v for v in videos
                  if needle in v.title.casefold()
                  or needle in " ".join(v.speakers).casefold()
                  or needle in v.description.casefold()]

    print(f"{conference.title} - {len(videos)} videos "
          f"(refs below work with \"import\")", file=sys.stderr)
    fkimport.print_table(
        ["ref", "date", "length", "title", "speakers"],
        [[v.ref, str(v.start or "")[:10], fkimport.duration(v.seconds), v.title,
          ", ".join(v.speakers)] for v in videos],
    )


def cmd_import(args, inventory: Inventory) -> None:
    scope = inventory.conference(args.event) if args.event else None

    def resolve(ref: str) -> Item:
        video = inventory.video(ref, scope)
        if video.non_free and not args.allow_non_free:
            raise Fatal(f"the inventory flags this recording as non-free "
                        f"({video.non_free}); pass --allow-non-free to publish "
                        f"it anyway")
        return to_item(video, ref, args.format)

    fkimport.import_items(args.video, resolve, args)


# --------------------------------------------------------------------------
# Entry point
# --------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="debconf-import.py",
        description="List the DebConf and Debian meeting recordings in the "
                    "Debian meetings archive, and publish one to Frikanalen "
                    "via fk.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="examples:\n"
               "  debconf-import.py events debconf2\n"
               "  debconf-import.py videos debconf25 --search kubernetes\n"
               "  debconf-import.py import debconf25#1 -c Kultur\n",
    )
    parser.add_argument("--refresh", action="store_true",
                        help="re-fetch the archive inventory even if the cached "
                             "copy is still fresh")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("events",
                       help="list the events the archive holds")
    p.add_argument("pattern", nargs="?",
                   help="only events whose name, title or location contains this")
    p.set_defaults(func=cmd_events)

    p = sub.add_parser("videos",
                       help="list the videos of one event")
    p.add_argument("event", help="e.g. debconf25, 2023/minidebconf-lisbon")
    p.add_argument("--search",
                   help="only videos matching this title, speaker or abstract")
    p.set_defaults(func=cmd_videos)

    p = sub.add_parser("import",
                       help="download a video at its best quality and upload it "
                            "to Frikanalen")
    p.add_argument("video", nargs="+",
                   help="a ref from \"videos\" (debconf25#1), a file name, a "
                        "title, or an archive URL (repeatable)")
    p.add_argument("--event",
                   help="resolve the refs within this event only")
    p.add_argument("--format", metavar="NAME",
                   help="use this encoding rather than the best one, e.g. lq "
                        "(names come from the inventory; \"default\" is the master)")
    p.add_argument("--allow-non-free", action="store_true",
                   help="publish even a recording the inventory flags as non-free")
    fkimport.add_import_arguments(p)
    p.set_defaults(func=cmd_import)

    return parser


def main(argv: list[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    args.func(args, Inventory(refresh=args.refresh))


if __name__ == "__main__":
    sys.exit(fkimport.run(main))
