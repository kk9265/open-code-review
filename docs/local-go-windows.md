# Windows project-local Go

This repository uses Go 1.26.9. The Python wrapper runs the pinned toolchain
without changing the system Go installation or the user's persistent Go settings.

On 2026-10-15, the official Windows amd64 archive was installed locally and its
SHA-256 checked against the Go download manifest. The wrapper reported Go 1.26.9,
the CLI built successfully, and the system installation remained Go 1.24.3.

From the repository root:

```powershell
python scripts/toolchain/go.py version
python scripts/toolchain/go.py env GOROOT GOPATH GOMODCACHE GOCACHE GOTOOLCHAIN
python scripts/toolchain/go.py build -o .\dist\opencodereview.exe .\cmd\opencodereview
```

The wrapper resolves paths relative to itself, runs from the repository root, and
leaves the caller's environment and working directory unchanged. It forwards Go
arguments and exit codes. Use Python 3.12 or newer for project maintenance tools.

The local SDK, when installed, is `.local-go/sdk/go`. If it is absent, the system
`go.exe` bootstraps Go 1.26.9 using Go's toolchain download mechanism. This requires
network access to the Go module proxy and checksum service. The wrapper sets
`GOENV=off` to isolate persistent user settings; process-level proxy settings still
apply.

All module and build caches live under `.local-go/`, which is ignored by Git and
excluded from Go's recursive package discovery because its name starts with a dot.
The project does not change `PATH`, the system Go installation, or `go env -w`
settings. An ordinary `go` command still uses the system installation and Go's
normal toolchain selection; use the wrapper for the isolated project environment.

For an IDE, select the absolute SDK path ending in `.local-go/sdk/go` as this
project's Go SDK. Configure its Go environment with the same values printed by
the wrapper if the IDE should share these caches.

Make is not required. Build and start the personal web workbench on Windows:

```powershell
python scripts/toolchain/go.py build -o .\dist\ocr-web.exe .\cmd\ocr-web
.\dist\ocr-web.exe
```

Only Windows and Linux are supported. On Linux, with Go installed, run from the
repository root to use the same pinned version and isolated caches:

```bash
CGO_ENABLED=0 python scripts/toolchain/go.py build -o ./dist/ocr-web ./cmd/ocr-web
./dist/ocr-web
```

Open http://127.0.0.1:5484/ and stop the foreground process with Ctrl+C. The
workbench currently supports repository registration, Gitea repository import and
background sync, review drafts, search, archive/restore, and settings. AI execution
is not connected yet. Pages default to Simplified Chinese.
There is no account system. Only loopback IP addresses are accepted.

Startup options (command-line flags override environment variables):

| Flag | Environment | Default |
| --- | --- | --- |
| `-addr` | `OCR_WEB_ADDR` | `127.0.0.1:5484` |
| `-data-dir` | `OCR_WEB_DATA_DIR` | `.ocr-data`, relative to the working directory |

Run from the repository root or pass an absolute data directory to reuse the same
database. For example: `.\dist\ocr-web.exe -data-dir E:\review-data`. The directory
is created automatically and migrations apply at startup. Do not put it on a
network share. For a backup, stop the process and copy the entire data directory;
restore that directory before restarting. Do not copy only the database while the
process is running, because SQLite also uses WAL files.

The Settings page persists the Gitea URL/user, model API base URL/name, review
language, and token budget. Defaults include `http://10.30.142.20:3000` and
`kk9265`. `OCR_GITEA_TOKEN` is used for Gitea API and HTTP Git access;
`OCR_LLM_API_KEY` remains reserved for AI integration. The Settings page shows only
whether the secrets are present. Saving settings does not contact either service.
Open the Gitea repositories page to verify the account and list repositories. Do
not put secrets in URLs.

To configure a private Gitea account on Windows, create a personal access token
with `read:user` and `read:repository` at
http://10.30.142.20:3000/user/settings/applications. Then run:

```powershell
python -m pip install -r scripts/requirements.txt
python scripts/workbench/start-web.py --gitea --restart
```

The script prompts for the token with hidden input, replaces any running
workbench from this project's executable, and runs the application in the
foreground. Wait for any active repository sync to finish before using
`--restart`. On first configuration, the token is saved as `gitea-token.dpapi` in
the data directory using Windows DPAPI encryption for the current Windows user.
Later launches automatically load it, even without `--gitea`; use
`python scripts/workbench/start-web.py` for everyday startup. Directly running the executable
does not load this saved credential. Use `--update-gitea-token` to replace the token,
or delete the encrypted file to forget it. An existing `OCR_GITEA_TOKEN` process
environment value takes precedence unless `--update-gitea-token` is supplied.
The encrypted file is ignored by Git and cannot be reused on another machine or
Windows account; configure a new token there. The script passes the decrypted
token only through the child process environment, leaving the caller unchanged.
Omit `--restart` if no workbench is running. Optional `--addr` and `--data-dir` flags
select the listening address and data directory. On Linux, the same Python entry
point accepts `OCR_GITEA_TOKEN` or hidden input with `--gitea`; hidden input is used
for that launch only and is not persisted. Existing Windows DPAPI files remain
compatible with the Python launcher and repository maintenance tool.

At http://127.0.0.1:5484/gitea, check the connection, search repositories, then
choose Import and fetch. Imports are idempotent by Gitea server and repository
ID. A single background worker fetches branches and tags into bare repositories
under `.ocr-data/repositories/`. The detail page shows queued/running/ready/error
states, the last successful sync, and a retry/fetch action. Use Refresh status to
see updates. Interrupted work is retained and can be retried manually. Empty
repositories cannot be imported until they have commits. No pushes, comment
writes, LFS downloads, or submodule fetches are performed. These caches are owned
by the application; the saved remote address and cache path cannot be edited.

Repository registration accepts an HTTP(S) repository page URL or an absolute
path to an existing Git checkout. Local paths require Git on PATH and are resolved
to the checkout root. Manual registration never clones or fetches; use the Gitea
import flow for managed synchronization. Review base/head
revisions are draft text until the execution feature is connected. Archiving a
repository hides it from the active list but retains its reviews.

`GET /healthz` checks the database connection. The existing CLI's read-only viewer
remains available separately through `opencodereview viewer` on port 5483.

When tests are requested, use `go test ./...` on Linux or
`python scripts/toolchain/go.py test ./...` on Windows, with `LC_ALL=C` in the environment.
The optional `-race` flag requires a suitable C compiler. No separate build system
or additional platform release matrix is maintained.
