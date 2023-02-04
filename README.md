# Frikanalen command line utility

This utility is intended to facilitate automated/bulk uploads.
It is currently being used primarily for development/testing.

If there is functionality you'd like to see, please file an issue - or even better, a PR.

## Requirements

ffmpeg is only required for test video generation.

## Installation

You need a Go compiler - and if you want to generate test media you'll need ffmpeg.

### Debian
```
sudo apt install golang ffmpeg
sudo make -e PREFIX=/usr install
```

### MacOS

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
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.43.0
```
