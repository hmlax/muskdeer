# Muskdeer lumberjack adaptation

Source: [natefinch/lumberjack v2.2.1](https://github.com/natefinch/lumberjack/tree/v2.2.1), commit `4cb27fcfbb0f35cb48c542c5ea80b7c1d18933d0`, MIT license retained in LICENSE (one trailing space normalized; legal text unchanged). Downloaded through the Go module proxy with module sum `h1:bBRl1b0OH9s/DuPhuXpNl+VtCaJXFZ5/uEFST95x9zc=`.

This package stays inside Muskdeer's single Go module. It does not replace the transitive upstream dependency used by other SDKs.

Original file SHA-256 values:

| File | SHA-256 |
|---|---|
| lumberjack.go | 63a614a1d02bfc2f14ac37f35943cb067936d78a243d262380ee260bf09572e3 |
| chown.go | b279f5d9cd0aff93297808cf4014caf4d55cbc02c8dcc60216403fcfd11ea634 |
| chown_linux.go | c85a7433bc7e55a3a19d74fb214fec91fc97106e84d9502d66dfa93beb073baf |
| LICENSE | 9622cbfd055d92991dcf523a256840424cc6356eab93beb4a93a973dec046492 |

Intentional changes:

- Run retention and gzip synchronously when opening/rotating, return their errors, and create no background worker. Upstream's mill worker has no shutdown API; its Close only closes the current file. This change makes instance ownership explicit.
- Make Close terminal and idempotent. Later writes/rotations fail with io.ErrClosedPipe.
- Reserve distinct millisecond backup names, including existing compressed backups, so rapid rotations do not overwrite earlier records.
- Remove global test hooks; keep time/file operations local. Use os.ReadDir/slices, wrap underlying errors, close files on all compression paths, and retain project formatting/lint checks.
- Create private log files and directories with permissions 0600/0750.
- Reject non-regular active paths and propagate stat/open errors instead of renaming an existing directory as if it were a log file.

Tradeoff: rotation, retention and compression add synchronous I/O to the write that triggers them. No throughput claim is made; callers choose a bounded file size and retention policy. Callers must give each process a distinct filename and freeze the configuration before concurrent use. This is a maintained source adaptation, not an unmodified upstream release.

Behavior tests cover gzip completion, terminal Close, rapid rotation, age/count retention, size boundaries, concurrent records and goroutine cleanup. Future upstream upgrades must reapply/review these changes and rerun these tests rather than overwrite this directory blindly.
