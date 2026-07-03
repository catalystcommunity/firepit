// /b/:slug (task C1 placeholder; real board/post-list view lands in C2).
import { useParams } from "@solidjs/router";
import type { Component } from "solid-js";
import NotBuilt from "~/components/NotBuilt";

const BoardPage: Component = () => {
  const params = useParams();
  return <NotBuilt title={`Board: ${params.slug}`} task="C2 (Boards + post lists)" />;
};

export default BoardPage;
