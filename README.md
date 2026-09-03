# FolderMirror

FolderMirror is a local web tool that builds a selective mirror of a directory tree. Directories are recreated normally; regular files become hardlinks, so the source and mirror use the same underlying data.

## Safety and limits

- Source and target must be separate, non-nested directories on the same filesystem/volume.
- Hardlinks are not backups: editing a file through either path edits the same file.
- Symbolic links and special files are skipped.
- Existing destination files are never overwritten.
- Stale files are removed only when their filesystem identity still matches the file originally managed by FolderMirror.
- The server listens on loopback by default. Do not expose it to a network: it has permission to modify the configured target.

## Run

```console
go run . -source /data/library -target /data/mirror
```

Open <http://127.0.0.1:8787>, choose folders, save, preview, and apply.

On Windows both paths must be on the same drive:

```powershell
go run . -source D:\Media -target D:\MediaView
```

Build a standalone executable with `go build -o foldermirror .` (or `go build -o foldermirror.exe .` on Windows).

## NixOS

Run directly from the repository:

```console
nix run . -- -source /data/library -target /data/mirror
```

Or install the flake package into a system or Home Manager configuration. The application uses only Go's standard library and has no runtime dependencies.

State is stored as `.foldermirror.json` inside the target by default. Folder rules use nearest-parent inheritance: selecting a parent includes its descendants, while a child can be explicitly excluded (and vice versa).
