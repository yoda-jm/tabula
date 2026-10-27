// Writes that can stop a show (the clock, the network settings, the card,
// initialize, shutdown…): every generic editor asks before sending them
// (the Browser page, mostly). One wrong click should not cost the show.

export interface Risk {
  title: string;
  /** what happens, said before the write goes */
  body: string;
  confirm: string;
}

const CLOCK: Risk = {
  title: "Change the console's clock?",
  body:
    "The audio stops while the console locks to the new clock or sample rate: every output goes silent for a moment, and a " +
    "recording through the card can break.",
  confirm: "Change the clock",
};

const NETWORK: Risk = {
  title: "Change the console's network settings?",
  body:
    "The console can take another address: tabula, the phones and every remote app (M32-Edit, Mixing Station…) lose it until " +
    "they connect to the new one.",
  confirm: "Change the network settings",
};

const CARD: Risk = {
  title: "Change the card's settings?",
  body:
    "The expansion card (DN32-USB…) restarts in the new mode: a recording through it stops, and the computer can see another " +
    "number of channels.",
  confirm: "Change the card",
};

const BACKUP = " tabula first saves the console's settings as a backup (Scenes → Scene files).";

/** ACTIONS are the /-action commands to confirm, when written with a value other than 0. */
const ACTIONS: Record<string, Risk> = {
  initall: {
    title: "Initialize the console?",
    body: "Every setting of the mix goes back to its default: the show set on the console is gone." + BACKUP,
    confirm: "Initialize",
  },
  initlib: {
    title: "Initialize the library?",
    body: "Every scene, snippet and preset stored in the console is erased. A scene file backup does not keep them.",
    confirm: "Erase the library",
  },
  initshow: {
    title: "Initialize the show?",
    body: "The show's cues, scenes and snippets are erased from the console. A scene file backup does not keep them.",
    confirm: "Erase the show",
  },
  doundo: {
    title: "Undo on the console?",
    body: "The console goes back to its undo checkpoint (/-undo/time): what changed since is lost." + BACKUP,
    confirm: "Undo",
  },
  formatcard: {
    title: "Format the X-Live! card's SD card?",
    body: "Everything recorded on it is erased.",
    confirm: "Format",
  },
  delsession: {
    title: "Delete an X-Live! session?",
    body: "The session recorded on the SD card is erased.",
    confirm: "Delete the session",
  },
  setip: NETWORK,
  setsrate: CLOCK,
};

/** RECALLS are the /-action commands that recall what their value names. */
const RECALLS: Record<string, string> = { goscene: "scene", gosnippet: "snippet", gocue: "cue" };

/**
 * riskOf tells whether writing value (a number, or the text typed) to addr
 * needs a confirmation, and what to say.
 */
export function riskOf(addr: string, value: number | string): Risk | null {
  const n = typeof value === "number" ? value : Number(String(value).trim());
  if (addr === "/-prefs/clocksource" || addr === "/-prefs/clockrate") return CLOCK;
  if (addr.startsWith("/-prefs/ip/")) return NETWORK;
  if (addr.startsWith("/-prefs/card/")) return CARD;
  if (addr === "/-stat/lock") {
    if (n === 2 || /^shut/i.test(String(value).trim())) {
      return { title: "Shut the console down?", body: "The console stops: no sound until it is switched on again.", confirm: "Shut down" };
    }
    if (n === 1) {
      return {
        title: "Lock the console?",
        body: "Its surface stops answering until it is unlocked on the console itself.",
        confirm: "Lock",
      };
    }
    return null;
  }
  if (!addr.startsWith("/-action/")) return null;
  const cmd = addr.slice("/-action/".length);
  if (RECALLS[cmd] && Number.isFinite(n)) {
    const what = RECALLS[cmd];
    return {
      title: `Recall ${what} ${n}?`,
      body: `The console's settings change as the ${what} holds them (its safes apply).` + BACKUP,
      confirm: "Recall",
    };
  }
  const risk = ACTIONS[cmd];
  return risk && n !== 0 ? risk : null;
}
