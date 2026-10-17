// The scribble strip: strip label and name in the console colour.

import type { Strip } from "../lib/strips";
import { colourOf } from "../lib/strips";
import { prettyText } from "../lib/values";
import { useValue } from "../store/hooks";

export function Scribble(props: {
  strip: Strip;
  nameAddr?: string;
  colorAddr?: string;
  selected?: boolean;
  onClick?(): void;
  large?: boolean;
}) {
  const name = useValue(props.nameAddr);
  const color = useValue(props.colorAddr);
  const c = colourOf(color === undefined ? 0 : Number(color.value));
  const text = name ? prettyText(name.text) : "";
  const cls = `scribble c-${c.name}${c.inverted ? " inv" : ""}${props.selected ? " selected" : ""}${props.large ? " large" : ""}`;
  const body = (
    <>
      <span className="scribble-label">{props.strip.label}</span>
      <span className="scribble-name">{text || " "}</span>
    </>
  );
  if (!props.onClick) return <div className={cls}>{body}</div>;
  return (
    <button
      type="button"
      className={cls}
      onClick={props.onClick}
      aria-pressed={props.selected}
      title={`Select ${props.strip.label}${text ? " (" + text + ")" : ""}`}
    >
      {body}
    </button>
  );
}
