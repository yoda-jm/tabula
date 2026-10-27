// Copying a strip's sections to other strips of its kind (POST
// /api/strips/copy): which sections a strip offers, and to which strips.

import type { CopySection } from "./api";
import type { Schema } from "./schema";
import type { Strip } from "./strips";
import { ALL_STRIPS, sectionsOf } from "./strips";

/** COPY_SECTIONS are the sections that can be copied, in the Channel page's order, with their labels. */
export const COPY_SECTIONS: { id: CopySection; label: string }[] = [
  { id: "preamp", label: "Preamp (trim, polarity, low cut; not the headamp's gain or 48 V)" },
  { id: "delay", label: "Delay" },
  { id: "gate", label: "Gate" },
  { id: "dyn", label: "Dynamics" },
  { id: "insert", label: "Insert" },
  { id: "eq", label: "EQ" },
  { id: "sends", label: "Sends" },
  { id: "main", label: "Main (mute, fader, pan…)" },
];

/** copyable tells whether a strip can be copied: L/R, M/C and the DCAs cannot. */
export const copyable = (s: Strip) => s.kind === "ch" || s.kind === "auxin" || s.kind === "fxrtn" || s.kind === "bus" || s.kind === "mtx";

/** copySectionsOf lists the sections of a strip that can be copied. */
export function copySectionsOf(schema: Schema, strip: Strip): CopySection[] {
  if (!copyable(strip)) return [];
  const has = new Set<string>(sectionsOf(schema, strip));
  return COPY_SECTIONS.map((c) => c.id).filter((id) => has.has(id));
}

/** copyTargets are the strips a strip copies to: the others of its kind. */
export const copyTargets = (strip: Strip) => (copyable(strip) ? ALL_STRIPS.filter((s) => s.kind === strip.kind && s.path !== strip.path) : []);
