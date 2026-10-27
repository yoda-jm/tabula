// A virtual soundcheck (docs/recording.md): a replay of a session with its
// tracks played back to the console's card. The console's inputs hear the
// card when its routing switch is on PLAY and the PLAY routing takes the
// card (CARD1-8…, as from the factory).

import type { AudioInfo } from "./audio";

export const ROUTSWITCH = "/config/routing/routswitch";
export const PLAY_BLOCKS = ["1-8", "9-16", "17-24", "25-32"];

export interface PlayRouting {
  /** the inputs come from the PLAY routing already */
  playing: boolean;
  /** the PLAY blocks taking the card: "1-8 ← CARD1-8" */
  card: string[];
}

/** playRouting reads the routing switch and the PLAY routing (text gives a parameter's node text). */
export function playRouting(text: (addr: string) => string | undefined): PlayRouting {
  const card = PLAY_BLOCKS.flatMap((b) => {
    const src = text(`/config/routing/PLAY/${b}`)?.trim() ?? "";
    return src.startsWith("CARD") ? [`${b} ← ${src}`] : [];
  });
  return { playing: text(ROUTSWITCH)?.trim() === "PLAY", card };
}

/** playableTracks counts a session's tracks that have a file to play. */
export function playableTracks(audio: AudioInfo | undefined): number {
  return audio?.tracks?.filter((t) => t.file).length ?? 0;
}
