# Testing tabula-emu with X32-Edit / M32-Edit

The emulator logs every request it does not handle. Connecting the official
editors to it is the fastest way to find what is still missing.

## Run the emulator

```sh
go build -o bin/ ./cmd/...
bin/tabula-emu -model M32C -scene testdata/scenes/demo.scn \
    -presets ~/.config/.M32-Edit/Xlib \
    -http :8032 -unhandled-log var/unhandled.jsonl
```

- `-model`: use `M32C`/`M32`/`M32R` for M32-Edit, `X32`/`X32RACK`/`X32C`… for X32-Edit.
- `-presets` fills the library with M32-Edit's factory presets, as a console ships with them.
- `-library var/library` keeps what you save from the editor's library pages, and offers the scene files you put there as `scene-00.scn`, `scene-01.scn`… in the console's scene slots (git ignores `var/`).
- `-usb testdata/usb` puts a USB stick in the console, for the editors' USB recorder page: its files are listed, its WAV files play (silently, the time running) and its scenes load.
- The emulator listens on UDP 10023 on every IPv4 address, so the editors'
  discovery finds it. On the same machine, connect the editor to this
  machine's LAN address (or 127.0.0.1 if the editor accepts it).

## Connect the editor

1. Start the editor and open its connection setup.
2. Pick the emulator in the discovered list, or type the IP address.
3. When asked which way to synchronise, choose **Mixer → PC** (load from the console), so the editor shows the emulator's scene.

## What to look at

Open <http://localhost:8032>, the read-only debug page:

- **Clients** — the editor should appear, subscribed to `/xremote`.
- **Unhandled requests** — every address the emulator did not understand, with counts and the last arguments. This is the to-do list. The same entries are appended to `var/unhandled.jsonl`.
- **Subscriptions** — the meters and subscriptions the editor asked for.
- **Recent traffic** — the last requests and replies (meters excluded).
- **State** — search any parameter, e.g. `ch/01/` or `IEM`, and watch it change while you move things in the editor.

Things worth checking in the editor: channel names, colours and icons; faders and mutes moving both ways (edit in the editor, then in a second editor or with `tabula`); routing pages; effect pages (every effect is modelled); library pages; meters moving.

## What is known to work with M32-Edit

Reading the emulator (Mixer → PC) and writing a scene to it (PC → Mixer),
meters, the RTA on the EQ page and in the graphic EQ editor, and linked
channels moving together: see [how the emulator works](emulator.md#verified-with-m32-edit).
An RTA or a meter that stays empty usually means the source has no signal:
in a fresh scene, faders and sends are down, so buses and matrices are silent.

## Testing without disturbing a running emulator

Only one program can use UDP port 10023 on a machine. To try a second
emulator and editor side by side (Linux), run them in a private network
namespace with a dummy interface, so the editor finds that emulator by
broadcast:

```sh
unshare -rn sh -c '
  ip link set lo up
  ip link add d0 type dummy && ip addr add 10.77.0.1/24 broadcast 10.77.0.255 dev d0 && ip link set d0 up
  bin/tabula-emu -name test -scene testdata/scenes/demo.scn -http 127.0.0.1:8099 &
  cd ~/m32-edit && HOME=/tmp/m32home XAUTHORITY=$HOME/.Xauthority ./M32-Edit'
```

`HOME` points the editor to a separate settings directory (copy
`~/.config/.M32-Edit` there first). To send OSC to that emulator, enter its
namespace: `nsenter -t <emulator pid> -U -n --preserve-credentials <command>`.

## Report

Send `var/unhandled.jsonl` (or the debug page's JSON at
<http://localhost:8032/api/unhandled>) — it is enough to implement what is
missing.
