# Frikanalen command line utility

This utility is intended to facilitate automated/bulk uploads.
It is currently being used primarily for development/testing.

If there is functionality you'd like to see, please file an issue - or even better, a PR.

## Configuration

Configuration lives in `~/.frikanalen.yaml`, created on first run.

The CLI talks to one *environment* at a time. `local` (`http://localhost:8000`, the
default), `staging` and `prod` are built in, and each keeps its own auth token -- so
switching back and forth does not mean logging in again:

```bash
fk env              # print the active environment
fk env list         # all environments, with the active one marked
fk env use staging  # switch
```

Run `fk login -e you@example.com` to authenticate; the token is stored under the active
environment.

`fk env use <name> --api <url>` defines an environment of your own (or points a built-in
one somewhere else). Setting `FK_API` overrides the active environment's URL for a single
run, without touching the configuration file.

The resulting file looks like this:

```yaml
environment: staging
environments:
  local:
    token: ...
  staging:
    token: ...
```

Configurations from older versions, which had a single top-level `api`/`token` pair, are
migrated to this layout automatically on first run.

## Importing from video archives

`contrib/` holds importers that pull videos out of a conference video archive and
publish them here. They share `contrib/fkimport.py`, and all work the same way: list
the events the archive holds, list one event's videos, then fetch one at its best
quality and hand it to `fk video create` -- so the environment and login you already
have (`fk env`, `fk login`) are what they publish to.

| script | archive | needs |
| --- | --- | --- |
| `contrib/voctoweb-import.py` | media.ccc.de, or any [voctoweb](https://github.com/voc/voctoweb) instance (`--api`) | stdlib only |
| `contrib/debconf-import.py` | the [Debian meetings archive](https://meetings-archive.debian.net/pub/debian-meetings/): DebConfs, MiniDebConfs and Debian meetings, 2004 onwards | PyYAML |

```bash
# 1. Which events are there?
contrib/voctoweb-import.py events congress
contrib/debconf-import.py events minidebconf

# 2. Which videos does one hold?
contrib/voctoweb-import.py videos 38c3 --search rekordbox
contrib/debconf-import.py videos debconf25 --search kubernetes

# 3. Download the best recording of a video and upload it
contrib/voctoweb-import.py import 98C007E2-A3B2-44FD-ADF5-D21224DE0988 -c Kultur
contrib/debconf-import.py import debconf25#39 -c Kultur
```

Both take several videos at a time. Title, description, speakers and a link back to
the source are carried over; the rest is yours to pass: `-c/--category` (Frikanalen
requires at least one; defaults to `Annet`), `-o/--org-id` and `-s/--series-id`, which
mean what they do for `fk video create`. Downloads go to a temporary directory that is
removed after a successful upload -- `--dir` keeps them somewhere of your choosing
instead, and an interrupted download resumes on the next run. `--dry-run` prints the
download URL and the exact `fk` command line without doing either.

### voctoweb

Mind the vocabulary where it meets the API: voctoweb calls an event a *conference*,
and reserves *event* for the individual talk -- which is why a video is addressed by
what media.ccc.de calls its event GUID, or by slug or `media.ccc.de/v/...` URL. The
recording picked is the one with voctoweb's `high_quality` flag, at the highest
resolution, preferring mp4 over webm, in the language the talk was held in;
`--container` and `--language` override that.

### The Debian meetings archive

There is no API, so this reads the video team's inventory of the archive from
[archive-meta](https://salsa.debian.org/debconf-video-team/archive-meta) -- one YAML
file per event, and the only place the real titles, abstracts and speaker lists live.
It is fetched once and cached for a day under `$XDG_CACHE_HOME/fk-cli`; `--refresh`
fetches it again.

Videos are addressed by the ref that `videos` prints (`2025/debconf25#39`), by file
name, by title, or by archive URL -- a title or file name that matches in several
events is reported with the candidates, and `--event` narrows the search. The
encoding picked is the highest-resolution one, which is the inventory's `default`
(the master) in every case today; `--format` picks another by name, e.g. `lq`.
Recordings the inventory flags as non-free are refused unless you pass
`--allow-non-free`.

Needs PyYAML: `apt install python3-yaml`, or `pip install pyyaml`.

## Requirements

ffmpeg is only required for test video generation.

### Refreshing the API schema and regenerating the client

The repo keeps a checked-in OpenAPI snapshot in `schema.yaml`, matching the pattern used in
`frikanalen/frontend` and `frikanalen/playout`. Two scripts manage schema updates and client
generation; the client itself is not committed (see `.gitignore`) and is regenerated by
`make`/CI before every build.

**Fetch the latest schema from the backend:**
```bash
./update-schema.sh
```

This fetches the current schema from `http://localhost:8000/api/schema/` and overwrites
`schema.yaml`. The backend must be running locally, or pass a different URL as the first
argument.

**Regenerate the Go client from the schema:**
```bash
./generate-client.sh
```

This runs `oapi-codegen` (via `go run`, so it isn't a project dependency) against
`schema.yaml`, writing the generated client into `fk-client/generated/`.

For local development, run both scripts in sequence after a backend change. `make fk` (and
`test`/`run`/`vet`/`lint`) always regenerates the client from the committed `schema.yaml`
first, so it can't silently drift from what's checked in; CI does the same.

## Installation

### Download a release binary

Prebuilt binaries for Linux, macOS and Windows are attached to every
[release](https://github.com/Frikanalen/fk-cli/releases). No Go toolchain needed, and
nothing to unpack -- the assets are the binaries themselves:

```bash
curl -fsSL -o fk https://github.com/Frikanalen/fk-cli/releases/latest/download/fk_linux_amd64
sudo install -m 755 fk /usr/local/bin/fk
```

Swap `fk_linux_amd64` for `fk_linux_arm64`, `fk_darwin_amd64`, `fk_darwin_arm64` or
`fk_windows_amd64.exe`. The `latest` URL always points at the newest release; substitute
`download/v1.0.0` for a specific one.

`SHA256SUMS` is published alongside the binaries if you want to verify the download.

Note that macOS will quarantine the unsigned binary on first run; clear it with
`xattr -d com.apple.quarantine fk`.

### Build from source

You need a Go compiler - and if you want to generate test media you'll need ffmpeg.

#### Debian
```
sudo apt install golang ffmpeg
sudo make -e PREFIX=/usr install
```

#### MacOS

```
brew install golang ffmpeg
make
sudo cp fk /usr/local/bin
```

## Linter

This codebase uses golangci-lint.

### MacOS

```
brew install golangci-lint
```

### Linux I guess:

```bash
curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.13.1
```

## Releasing

Versioning is handled by [release-please](https://github.com/googleapis/release-please),
driven by [Conventional Commit](https://www.conventionalcommits.org/) messages on `main`.
It keeps a release PR open with the pending version bump and CHANGELOG entry; merging
that PR is what cuts a release.

Merging the release PR tags the commit and creates the GitHub release, and the build
workflow then calls the [release workflow](.github/workflows/release.yaml) to attach the
binaries: it regenerates the client, runs vet and the tests, cross-compiles for
linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 and windows/amd64, and uploads the
bare binaries plus a `SHA256SUMS` file to the release.

The call is explicit because release-please tags using the default `GITHUB_TOKEN`, and
events raised by that token do not start new workflow runs -- so the tag push cannot
trigger the release workflow by itself.

The tag name is stamped into the binary, so `fk --version` reports it.

Pushing a `v`-prefixed tag by hand also builds and publishes a release, which is mostly
useful outside the release-please flow; a tag with a suffix (`v1.0.0-rc1`) goes out as a
prerelease. If a release build needs to be re-run, use the release workflow's
`workflow_dispatch` trigger with the existing tag rather than deleting and re-pushing it
-- the assets are re-uploaded over the existing release.
