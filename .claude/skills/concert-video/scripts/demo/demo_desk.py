# The engineer of the demo session: OSC messages to the emulator, as a
# console's own surface would change it (tabula records them as console
# moves), and the setlist's marks through tabula, timed from the start of
# the recording to match demo_audio.py.
#   demo_desk.py setup EMU_ADDR              the mix before the show (the demo scene has every fader down)
#   demo_desk.py show EMU_ADDR TABULA_URL    the show's moves and marks (about 88 s)
import sys, socket, struct, time, math, json, urllib.request

def osc(addr, *args):
    pad = lambda b: b + b"\0" * (4 - len(b) % 4)
    tags = "," + "".join("f" if isinstance(a, float) else "i" for a in args)
    data = b"".join(struct.pack(">f", a) if isinstance(a, float) else struct.pack(">i", a) for a in args)
    return pad(addr.encode()) + pad(tags.encode()) + data

host, port = sys.argv[2].rsplit(":", 1)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
send = lambda addr, *a: sock.sendto(osc(addr, *a), (host, int(port)))

# the console's value scales (normalised 0..1)
def fader(db):
    if db <= -90: return 0.0
    return (db + 90) / 480 if db < -60 else (db + 70) / 160 if db < -30 else (db + 50) / 80 if db < -10 else (db + 30) / 40
gain = lambda db: (db + 15) / 30
freq = lambda f: math.log(f / 20) / math.log(1000)
hpf = lambda f: math.log(f / 20) / math.log(20)
thr = lambda db: (db + 60) / 60
RATIO = [1.1, 1.3, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0, 7.0, 10.0, 20.0, 100.0]

def ramp(addr, scale, a, b, seconds, steps=12):
    for i in range(1, steps + 1):
        send(addr, scale(a + (b - a) * i / steps)); time.sleep(seconds / steps)

if sys.argv[1] == "setup":
    for ch, db in {1: -10, 2: -5, 3: -12, 6: -6, 8: -4, 9: -3, 10: -8, 12: 0, 13: -1, 14: -3, 16: -6}.items():
        send(f"/ch/{ch:02d}/mix/fader", fader(db))
    for r, db in {1: -10, 3: -12, 5: -14}.items():
        send(f"/fxrtn/{r:02d}/mix/fader", fader(db))
    send("/fxrtn/07/mix/on", 0)  # the chorus stays off: hidden
    for d in (1, 2, 3, 4): send(f"/dca/{d}/fader", fader(0))
    send("/main/st/mix/fader", fader(0)); send("/mtx/01/mix/fader", fader(0)); send("/mtx/03/mix/fader", fader(-6))
    for ch in (12, 13):
        send(f"/ch/{ch:02d}/preamp/hpon", 1); send(f"/ch/{ch:02d}/preamp/hpf", hpf(100))
        send(f"/ch/{ch:02d}/eq/on", 1)
        for band, (f, g) in {1: (200, 0), 2: (300, -4), 3: (3000, 2)}.items():
            send(f"/ch/{ch:02d}/eq/{band}/type", 2); send(f"/ch/{ch:02d}/eq/{band}/f", freq(f)); send(f"/ch/{ch:02d}/eq/{band}/g", gain(g))
    send("/ch/12/dyn/on", 1); send("/ch/12/dyn/thr", thr(-20)); send("/ch/12/dyn/ratio", RATIO.index(3.0))
    send("/ch/09/eq/on", 1); send("/ch/09/eq/3/type", 2); send("/ch/09/eq/3/f", freq(3000)); send("/ch/09/eq/3/g", gain(0))
    send("/ch/02/preamp/hpon", 1); send("/ch/02/preamp/hpf", hpf(40))
    time.sleep(0.5)
    sys.exit()

url = sys.argv[3].rstrip("/")
def mark():
    urllib.request.urlopen(urllib.request.Request(url + "/api/record/mark", data=json.dumps({"next": True}).encode(),
                                                  headers={"Content-Type": "application/json"})).read()
t0 = time.monotonic()
def at(t):
    d = t - (time.monotonic() - t0)
    if d > 0: time.sleep(d)
at(12); mark()                                                               # the first song, marked a little late
at(15); ramp("/ch/12/mix/fader", fader, 0, 2, 2)                            # bring up the lead voice
at(20); ramp("/ch/12/eq/2/g", gain, -4, -8, 1.5); ramp("/ch/12/eq/2/f", freq, 300, 250, 1.5)  # cut the low mids more
at(26); ramp("/ch/09/eq/3/g", gain, 0, 3, 1.5)                               # presence on the acoustic guitar
at(30); ramp("/ch/12/dyn/thr", thr, -20, -26, 1.5)                          # compress the voice harder
at(38); ramp("/ch/06/mix/fader", fader, -6, -10, 2)                         # the autoharp down
at(43); send("/ch/01/mix/on", 0)                                            # no hang in the second song
at(53); mark()
at(56); ramp("/ch/13/eq/1/g", gain, 0, -5, 2)                               # cut the low mids on the second voice
at(62); ramp("/ch/08/mix/fader", fader, -4, -1, 2)                          # the electric guitar up
at(70); ramp("/dca/4/fader", fader, 0, -4, 1.5)                             # the effects down
at(78); ramp("/ch/02/preamp/hpf", hpf, 40, 90, 1.5)                         # more low cut on the cajon
at(88)
