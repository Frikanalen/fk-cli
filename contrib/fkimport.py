"""Shared machinery for the contrib import scripts.

The scripts in this directory all do the same three things -- list what a video
archive holds, list one collection's videos, then fetch one at its best quality
and hand it to "fk video create". Only the archive's API differs, so everything
downstream of "here is a file to publish" lives here.

A script's job is to turn a user's reference into an Item; import_items() does
the rest.
"""

from __future__ import annotations

import argparse
import json
import os
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

# Frikanalen requires at least one category on every video; "Annet" (other) is
# the one that fits an arbitrary conference talk. "fk video create -c" takes the
# category's name, and the full list lives at <https://frikanalen.no/api/categories>.
DEFAULT_CATEGORY = "Annet"

TITLE_MAX = 255  # Frikanalen's limit on a video's name

USER_AGENT = "fk-cli contrib importer"


class Fatal(Exception):
    """An error worth reporting to the user without a traceback."""


class Item:
    """One video, resolved down to what publishing it actually needs."""

    def __init__(self, ref: str, title: str, description: str, url: str,
                 filename: str, note: str = "", warning: str = "",
                 language: str = "") -> None:
        self.ref = ref
        self.title = truncate_title(title)
        self.description = description
        self.url = url
        self.filename = filename
        # note describes the chosen file (resolution, codec, size); warning is
        # something the user should see before it goes out on air.
        self.note = note
        self.warning = warning
        # The language the talk was held in, where the archive knows it: which
        # audio track to keep when the file carries several.
        self.language = language


def truncate_title(title: str) -> str:
    title = (title or "").strip()
    if len(title) > TITLE_MAX:
        title = title[: TITLE_MAX - 1].rstrip() + "…"
    return title


# --------------------------------------------------------------------------
# HTTP
# --------------------------------------------------------------------------


def get(url: str, accept: str = "application/json") -> bytes:
    """Fetch a URL, turning the usual failures into Fatal. 404 raises KeyError,
    which callers use to tell "no such thing" from "the server is unhappy"."""
    req = urllib.request.Request(
        url, headers={"User-Agent": USER_AGENT, "Accept": accept})
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return resp.read()
    except urllib.error.HTTPError as err:
        if err.code == 404:
            raise KeyError(url) from err
        raise Fatal(f"{url}: HTTP {err.code} {err.reason}") from err
    except urllib.error.URLError as err:
        raise Fatal(f"{url}: {err.reason}") from err


def human_bytes(n: float) -> str:
    for unit in ("B", "KiB", "MiB", "GiB", "TiB"):
        if abs(n) < 1024 or unit == "TiB":
            return f"{n:.0f} {unit}" if unit == "B" else f"{n:.1f} {unit}"
        n /= 1024
    return f"{n:.1f} TiB"


def download(url: str, dest: str) -> str:
    """Fetch url to dest, resuming a previous attempt when one is lying around.

    Downloads land in "<dest>.part" and are renamed on completion, so a
    complete file at dest is always a complete download.
    """
    if os.path.exists(dest):
        print(f"Already downloaded: {dest}", file=sys.stderr)
        return dest

    part = dest + ".part"
    have = os.path.getsize(part) if os.path.exists(part) else 0

    headers = {"User-Agent": USER_AGENT}
    if have:
        headers["Range"] = f"bytes={have}-"

    req = urllib.request.Request(url, headers=headers)
    try:
        resp = urllib.request.urlopen(req, timeout=60)
    except urllib.error.HTTPError as err:
        if err.code == 416 and have:  # asked past the end: start over
            os.remove(part)
            return download(url, dest)
        raise Fatal(f"{url}: HTTP {err.code} {err.reason}") from err
    except urllib.error.URLError as err:
        raise Fatal(f"{url}: {err.reason}") from err

    with resp:
        resuming = resp.status == 206
        if have and not resuming:
            have = 0  # server ignored the range; take it from the top
        total = int(resp.headers.get("Content-Length") or 0) + have
        mode = "ab" if resuming else "wb"

        label = os.path.basename(dest)
        reporter = _Progress(label, have, total)
        with open(part, mode) as fh:
            while chunk := resp.read(1 << 20):
                fh.write(chunk)
                reporter.advance(len(chunk))
        reporter.finish()

    if total and os.path.getsize(part) != total:
        raise Fatal(f"{label}: truncated download, retry to resume it")

    os.replace(part, dest)
    return dest


class _Progress:
    """Download progress on stderr: a redrawn line on a terminal, occasional
    lines otherwise, so logs from an unattended run stay readable."""

    def __init__(self, label: str, done: int, total: int) -> None:
        self.label = label
        self.done = done
        self.total = total
        self.tty = sys.stderr.isatty()
        self.started = time.monotonic()
        self.last = 0.0
        print(f"Downloading {label} ({human_bytes(total) if total else 'unknown size'})",
              file=sys.stderr)

    def advance(self, n: int) -> None:
        self.done += n
        now = time.monotonic()
        if now - self.last < (0.2 if self.tty else 15):
            return
        self.last = now
        self._draw(end="\r" if self.tty else "\n")

    def _draw(self, end: str) -> None:
        rate = self.done / max(time.monotonic() - self.started, 1e-6)
        if self.total:
            line = (f"  {100 * self.done / self.total:5.1f}%  "
                    f"{human_bytes(self.done)} / {human_bytes(self.total)}"
                    f"  {human_bytes(rate)}/s")
        else:
            line = f"  {human_bytes(self.done)}  {human_bytes(rate)}/s"
        print(f"{line:<60}", end=end, file=sys.stderr, flush=True)

    def finish(self) -> None:
        self._draw(end="\n")


# --------------------------------------------------------------------------
# Normalising what was downloaded
# --------------------------------------------------------------------------

# Conference recordings often carry more than one of each: a camera mix and a
# slide capture, an original language and its interpretations. What survives
# ingest is then whatever the transcoder's stream selection happened to prefer,
# which is not a thing to leave to chance on something being broadcast. So the
# file is cut down to one video and one audio track before it is uploaded.


def probe_streams(path: str) -> list[dict]:
    ffprobe = shutil.which("ffprobe")
    if not ffprobe:
        raise Fatal("ffprobe not found on $PATH; install ffmpeg, or pass "
                    "--no-normalize to upload the file untouched")
    result = subprocess.run(
        [ffprobe, "-v", "error", "-show_streams", "-print_format", "json", path],
        capture_output=True, text=True)
    if result.returncode != 0:
        raise Fatal(f"ffprobe failed on {os.path.basename(path)}: "
                    f"{result.stderr.strip().splitlines()[-1] if result.stderr.strip() else result.returncode}")
    return json.loads(result.stdout).get("streams", [])


def _stream_language(stream: dict) -> str:
    return str((stream.get("tags") or {}).get("language") or "").casefold()


def _stream_title(stream: dict) -> str:
    return str((stream.get("tags") or {}).get("title") or "")


def _pick_stream(streams: list[dict], language: str = "") -> dict | None:
    """Choose one stream: the asked-for language, else the one the container
    itself marks default, else the first."""
    if not streams:
        return None
    if language:
        wanted = language.casefold()
        tagged = [s for s in streams
                  if _stream_language(s) and (
                      _stream_language(s) == wanted
                      or _stream_language(s).startswith(wanted[:2])
                      or wanted.startswith(_stream_language(s)[:2]))]
        if tagged:
            streams = tagged
    default = [s for s in streams
               if (s.get("disposition") or {}).get("default")]
    return (default or streams)[0]


def _describe(stream: dict) -> str:
    bits = [f"#{stream.get('index')}", str(stream.get("codec_name") or "?")]
    if stream.get("width"):
        bits.append(f"{stream['width']}x{stream['height']}")
    if language := _stream_language(stream):
        bits.append(language)
    if title := _stream_title(stream):
        bits.append(f"\"{title}\"")
    return " ".join(bits)


def normalize(path: str, language: str = "") -> None:
    """Reduce path to a single video and audio track, in place.

    Nothing is re-encoded, and a file that already holds one of each is left
    alone -- so this is a no-op for most archives, and cheap when it is not.
    """
    streams = probe_streams(path)
    # Cover art rides along as a video stream; it is not one of the candidates.
    video = [s for s in streams if s.get("codec_type") == "video"
             and not (s.get("disposition") or {}).get("attached_pic")]
    audio = [s for s in streams if s.get("codec_type") == "audio"]
    other = [s for s in streams if s.get("codec_type") not in ("video", "audio")]
    if len(video) <= 1 and len(audio) <= 1 and not other:
        return

    keep_video = _pick_stream(video)
    keep_audio = _pick_stream(audio, language)
    if keep_video is None:
        raise Fatal(f"{os.path.basename(path)} has no video track")
    if audio and keep_audio is not None and language and \
            not _stream_language(keep_audio).startswith(language.casefold()[:2]):
        print(f"  no {language!r} audio track; keeping "
              f"{_describe(keep_audio)}", file=sys.stderr)

    kept = [keep_video] + ([keep_audio] if keep_audio else [])
    print("  normalizing: keeping " + ", ".join(_describe(s) for s in kept)
          + f", dropping {len(streams) - len(kept)}", file=sys.stderr)

    ffmpeg = shutil.which("ffmpeg")
    if not ffmpeg:
        raise Fatal("ffmpeg not found on $PATH; install it, or pass "
                    "--no-normalize to upload the file untouched")

    root, extension = os.path.splitext(path)
    output = f"{root}.normalized{extension}"
    argv = [ffmpeg, "-nostdin", "-v", "error", "-y", "-i", path]
    for stream in kept:
        argv += ["-map", f"0:{stream['index']}"]
    argv += ["-c", "copy"]
    if extension.lower() in (".mp4", ".m4v", ".mov"):
        argv += ["-movflags", "+faststart"]
    argv.append(output)

    result = subprocess.run(argv, capture_output=True, text=True)
    if result.returncode != 0:
        if os.path.exists(output):
            os.remove(output)
        detail = result.stderr.strip().splitlines()
        raise Fatal(f"ffmpeg could not normalize {os.path.basename(path)}: "
                    f"{detail[-1] if detail else result.returncode}")

    # Take the original's name, so the upload and any kept file are still
    # recognisable, and so a re-run finds nothing left to do.
    os.replace(output, path)


# --------------------------------------------------------------------------
# Handing files to fk
# --------------------------------------------------------------------------


def fk_create(fk_bin: str, path: str, item: Item, args: argparse.Namespace) -> None:
    argv = [fk_bin, "video", "create",
            "-t", item.title,
            "-d", item.description,
            "-f", path]
    for category in args.category or [DEFAULT_CATEGORY]:
        argv += ["-c", category]
    if args.org_id is not None:
        argv += ["-o", str(args.org_id)]
    if args.series_id is not None:
        argv += ["-s", str(args.series_id)]
    if args.no_wait:
        argv.append("--wait=false")

    if args.dry_run:
        print("would run: " + shlex.join(argv))
        return

    result = subprocess.run(argv)
    if result.returncode != 0:
        raise Fatal(f"fk video create exited with status {result.returncode}")


def add_import_arguments(parser: argparse.ArgumentParser) -> None:
    """The half of an import command's flags that is about Frikanalen, not
    about the archive being imported from."""
    parser.add_argument("-c", "--category", action="append", metavar="NAME",
                        help=f"Frikanalen category, repeatable "
                             f"(default: {DEFAULT_CATEGORY})")
    parser.add_argument("-o", "--org-id", type=int,
                        help="Frikanalen organization ID (see \"fk org list\"); "
                             "only needed if you edit more than one")
    parser.add_argument("-s", "--series-id", type=int,
                        help="Frikanalen series to file the videos under "
                             "(see \"fk series list\")")
    parser.add_argument("--dir", metavar="PATH",
                        help="download into this directory and keep the files "
                             "(default: a temporary directory, removed afterwards)")
    parser.add_argument("--keep", action="store_true",
                        help="keep the downloaded file after a successful upload")
    parser.add_argument("--no-wait", action="store_true",
                        help="do not wait for Frikanalen's ingest to finish")
    parser.add_argument("--no-normalize", action="store_true",
                        help="upload the file as it was published, rather than "
                             "first reducing it to one video and one audio track")
    parser.add_argument("--audio-language", metavar="LANG",
                        help="keep this audio track when the recording carries "
                             "several, e.g. eng (default: the language the talk "
                             "was held in)")
    parser.add_argument("--dry-run", action="store_true",
                        help="show what would be downloaded and run, without doing it")
    parser.add_argument("--fk", default=os.environ.get("FK_BIN", "fk"),
                        help="path to the fk binary (default: fk)")


def import_items(refs: list[str], resolve, args: argparse.Namespace) -> None:
    """Publish each ref: resolve it to an Item, download it, hand it to fk.

    resolve(ref) -> Item may raise Fatal for a ref that cannot be published;
    the remaining refs are still attempted, and the failures are counted.
    """
    fk_bin = shutil.which(args.fk)
    if not fk_bin and not args.dry_run:
        raise Fatal(f"{args.fk}: not found on $PATH (build it with make, or pass --fk)")

    # A named directory is the user's own; only a temporary one gets cleaned up.
    directory = args.dir or tempfile.mkdtemp(prefix="fk-import-")
    os.makedirs(directory, exist_ok=True)
    keep = args.keep or bool(args.dir) or args.dry_run

    failures = 0
    for ref in refs:
        try:
            item = resolve(ref)
            print(f"\n{item.title}", file=sys.stderr)
            if item.note:
                print(f"  {item.note}", file=sys.stderr)
            if item.warning:
                print(f"  warning: {item.warning}", file=sys.stderr)

            path = os.path.join(directory, os.path.basename(item.filename))
            if args.dry_run:
                print(f"would download: {item.url} -> {path}")
            else:
                download(item.url, path)
                if not args.no_normalize:
                    normalize(path, args.audio_language or item.language)

            fk_create(fk_bin or args.fk, path, item, args)

            if not keep:
                os.remove(path)
        except Fatal as err:
            failures += 1
            print(f"error: {ref}: {err}", file=sys.stderr)

    # A temporary directory is only worth keeping if a failed run left
    # something half-downloaded in it, which a re-run would resume.
    leftovers = bool(failures and os.listdir(directory))
    if not args.dir and not leftovers:
        shutil.rmtree(directory, ignore_errors=True)
    if failures:
        message = f"{failures} of {len(refs)} video(s) failed"
        if leftovers:
            message += f" (partial downloads kept in {directory})"
        raise Fatal(message)


# --------------------------------------------------------------------------
# Output
# --------------------------------------------------------------------------


def print_table(headers: list[str], rows: list[list[str]]) -> None:
    if not rows:
        print("(nothing to show)", file=sys.stderr)
        return
    widths = [len(h) for h in headers]
    for row in rows:
        for i, cell in enumerate(row):
            widths[i] = max(widths[i], len(cell))

    # Let the last column soak up whatever width is left over.
    budget = shutil.get_terminal_size((100, 24)).columns
    fixed = sum(widths[:-1]) + 2 * len(widths)
    widths[-1] = max(20, min(widths[-1], budget - fixed))

    def fmt(cells: list[str]) -> str:
        out = []
        for cell, width in zip(cells, widths):
            if len(cell) > width:
                cell = cell[: width - 1] + "…"
            out.append(cell.ljust(width))
        return "  ".join(out).rstrip()

    print(fmt(headers))
    print(fmt(["-" * w for w in widths]))
    for row in rows:
        print(fmt(row))


def duration(seconds) -> str:
    seconds = int(seconds or 0)
    return f"{seconds // 3600}:{seconds // 60 % 60:02d}:{seconds % 60:02d}"


def run(main_func, argv=None) -> int:
    """Turn the expected failures into exit statuses instead of tracebacks."""
    try:
        main_func(argv)
    except Fatal as err:
        print(f"error: {err}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("\ninterrupted", file=sys.stderr)
        return 130
    except BrokenPipeError:
        return 0
    return 0
