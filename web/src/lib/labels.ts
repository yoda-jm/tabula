// Human labels for parameter fields, by "<node>/<field>" (the last segment
// of the node path and the field name), then by field name alone.

const BY_NODE: Record<string, string> = {
  "config/name": "Name",
  "config/icon": "Icon",
  "config/color": "Colour",
  "config/source": "Source",
  "preamp/trim": "Trim",
  "preamp/invert": "Polarity invert",
  "preamp/hpon": "Low cut",
  "preamp/hpslope": "Low cut slope",
  "preamp/hpf": "Low cut freq",
  "delay/on": "Delay",
  "delay/time": "Delay time",
  "gate/on": "Gate",
  "gate/mode": "Mode",
  "gate/thr": "Threshold",
  "gate/range": "Range",
  "gate/attack": "Attack",
  "gate/hold": "Hold",
  "gate/release": "Release",
  "gate/keysrc": "Key source",
  "dyn/on": "Compressor",
  "dyn/mode": "Mode",
  "dyn/det": "Detector",
  "dyn/env": "Envelope",
  "dyn/thr": "Threshold",
  "dyn/ratio": "Ratio",
  "dyn/knee": "Knee",
  "dyn/mgain": "Make-up gain",
  "dyn/attack": "Attack",
  "dyn/hold": "Hold",
  "dyn/release": "Release",
  "dyn/pos": "Position",
  "dyn/keysrc": "Key source",
  "dyn/mix": "Mix",
  "dyn/auto": "Auto time",
  "filter/on": "Key filter",
  "filter/type": "Key filter type",
  "filter/f": "Key filter freq",
  "insert/on": "Insert",
  "insert/pos": "Position",
  "insert/sel": "FX slot",
  "eq/on": "EQ",
  "mix/on": "On (not muted)",
  "mix/fader": "Fader",
  "mix/st": "To LR",
  "mix/pan": "Pan",
  "mix/mono": "To mono",
  "mix/mlevel": "Mono level",
  "grp/dca": "DCA groups",
  "grp/mute": "Mute groups",
  "automix/group": "Automix group",
  "automix/weight": "Automix weight",
};

const BY_FIELD: Record<string, string> = {
  type: "Type",
  f: "Frequency",
  g: "Gain",
  q: "Q",
  on: "On",
  level: "Level",
  pan: "Pan",
  fader: "Fader",
  panFollow: "Pan follow",
};

export function fieldLabel(nodePath: string, name: string, label?: string): string {
  if (label) return label;
  const node = nodePath.slice(nodePath.lastIndexOf("/") + 1);
  return BY_NODE[`${node}/${name}`] ?? BY_FIELD[name] ?? name;
}
