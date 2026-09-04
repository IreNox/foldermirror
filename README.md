# FolderMirror

FolderMirror is a local web tool for organizing a storage tree with hardlinks. It can build a selective mirror and collect wildcard-matched files from a separate imports tree.

## Safety and limits

- Storage, mirror, and optional imports roots must be separate directories on the same filesystem/volume.
- Hardlinks are not backups: editing a file through either path edits the same file.
- Symbolic links and special files are skipped.
- Existing destination files are never overwritten.
- Stale files are removed only when their filesystem identity still matches the file originally managed by FolderMirror.
- The server listens on loopback by default. Do not expose it to a network: it has permission to modify the configured storage and mirror.

## Run

```console
go run . -storage /data/library -mirror /data/mirror -imports /data/downloads
```

Open <http://127.0.0.1:8787>, choose folders, save, preview, and apply.

On Windows both paths must be on the same drive:

```powershell
go run . -storage D:\Media -mirror D:\MediaView -imports D:\Downloads
```

Build a standalone executable with `go build -o foldermirror .` (or `go build -o foldermirror.exe .` on Windows).

## NixOS

Run directly from the repository:

```console
nix run . -- -storage /data/library -mirror /data/mirror -imports /data/downloads
```

Or install the flake package into a system or Home Manager configuration. The application uses only Go's standard library and has no runtime dependencies.

State is stored as `.foldermirror.json` inside the mirror by default. Folder rules use nearest-parent inheritance: selecting a parent includes its descendants, while a child folder or individual file can be explicitly excluded (and vice versa).

The optional **Collect files** mode scans the imports root or a selected subfolder recursively. Choose the import location and an existing storage destination from collapsible trees. A filename wildcard such as `*.mkv` is matched case-insensitively against each basename, and matching files are hardlinked beneath the storage selection while preserving their relative paths. The 12 most recently used unique wildcards are saved for autocomplete. Existing different files are reported as conflicts and never overwritten.

The older `-source` and `-target` flags remain available as aliases for `-storage` and `-mirror`.
