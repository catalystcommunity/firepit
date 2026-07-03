// A placeholder for a route C2-C4 will replace (task C1, PLANDOC.md §7:
// "placeholder routes ... rendering 'not built yet' stubs"). Keeping this as
// one shared component (rather than copy-pasted markup per page) means
// there's exactly one place to delete text from as each real screen lands.
import type { Component } from "solid-js";

export interface NotBuiltProps {
  title: string;
  /** Which task/wave builds the real screen — shown so it's obvious this is expected, not broken. */
  task: string;
}

const NotBuilt: Component<NotBuiltProps> = (props) => (
  <div class="not-built">
    <h2>{props.title}</h2>
    <p>Not built yet — lands in {props.task}.</p>
  </div>
);

export default NotBuilt;
