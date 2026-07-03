// /b/:slug/p/:postId (task C1 placeholder; real thread view lands in C3).
import { useParams } from "@solidjs/router";
import type { Component } from "solid-js";
import NotBuilt from "~/components/NotBuilt";

const ThreadPage: Component = () => {
  const params = useParams();
  return <NotBuilt title={`Thread: ${params.slug}/${params.postId}`} task="C3 (Thread view)" />;
};

export default ThreadPage;
