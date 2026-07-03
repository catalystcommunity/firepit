// /b/:slug/p/:postId — the thread view (task C3, PLANDOC.md §7). The core
// screen: post header + body, the full comment tree (or flat mailing-list
// order), reply-at-any-depth, endorsements, edit/delete-own-content with
// revision history, tombstones, read marks, and comment permalinks. See
// src/components/thread/ for the component breakdown.
import { A, useParams } from "@solidjs/router";
import { createEffect, createMemo, createResource, createSignal, onCleanup, Show, type Component } from "solid-js";
import { api } from "~/lib/api";
import { extractMentionHandles } from "~/lib/markdown";
import { useSession } from "~/lib/session";
import ContentCard from "~/components/thread/ContentCard";
import CommentTree from "~/components/thread/CommentTree";
import Composer from "~/components/thread/Composer";
import type { ThreadActions } from "~/components/thread/threadActions";
import {
  isUnreadOverride as readIsUnreadOverride,
  readLastViewed,
  setUnreadOverride,
  writeLastViewed,
} from "~/components/thread/readMarks";
import { readViewMode, writeViewMode, type ThreadViewMode } from "~/components/thread/viewMode";

// How long the thread must stay visibly open before the post's read
// watermark auto-advances (task C3 scope item 5).
const AUTO_MARK_READ_MS = 2000;

const ThreadPage: Component = () => {
  const params = useParams<{ slug: string; postId: string }>();
  const session = useSession();

  const [threadRes, { refetch }] = createResource(() => params.postId, (postId) => api.thread.getThread({ postId }));

  const [mode, setMode] = createSignal<ThreadViewMode>(readViewMode());
  const toggleMode = (): void => {
    const next: ThreadViewMode = mode() === "tree" ? "flat" : "tree";
    setMode(next);
    writeViewMode(next);
  };

  // --- read marks (task C3 scope item 5) ---------------------------------
  const [baseline, setBaseline] = createSignal<Date | null>(readLastViewed(params.postId));
  const [overrideTick, setOverrideTick] = createSignal(0); // bumped to re-read localStorage overrides
  const [scrolledForHash, setScrolledForHash] = createSignal(false);

  createEffect(() => {
    const postId = params.postId; // reset per-thread state when navigating to a different post
    setBaseline(readLastViewed(postId));
    setOverrideTick((v) => v + 1);
    setScrolledForHash(false);
  });

  createEffect(() => {
    const postId = params.postId;
    const timer = setTimeout(() => {
      if (!session.user()) return;
      void api.read.markRead({ targetType: "post", targetId: postId }).then(() => {
        writeLastViewed(postId, new Date());
      });
    }, AUTO_MARK_READ_MS);
    onCleanup(() => clearTimeout(timer));
  });

  // Permalink scroll+highlight on load (task C3 scope item 5).
  createEffect(() => {
    const thread = threadRes();
    if (!thread || scrolledForHash()) return;
    setScrolledForHash(true);
    const hash = typeof window !== "undefined" ? window.location.hash : "";
    if (!hash) return;
    queueMicrotask(() => {
      const el = typeof document !== "undefined" ? document.getElementById(hash.slice(1)) : null;
      if (!el) return;
      el.scrollIntoView?.({ behavior: "smooth", block: "center" });
      el.classList.add("permalink-highlight");
      setTimeout(() => el.classList.remove("permalink-highlight"), 2500);
    });
  });

  const mentionCandidates = createMemo(() => {
    const thread = threadRes();
    if (!thread) return [];
    const all = [thread.post.bodyMd, ...thread.comments.map((c) => c.bodyMd)].join("\n");
    return extractMentionHandles(all);
  });

  const isUnread = (kind: "post" | "comment", id: string, createdAt: Date): boolean => {
    overrideTick();
    if (readIsUnreadOverride(kind, id)) return true;
    const b = baseline();
    return b !== null && createdAt.getTime() > b.getTime();
  };

  const isUnreadOverride = (kind: "post" | "comment", id: string): boolean => {
    overrideTick();
    return readIsUnreadOverride(kind, id);
  };

  const toggleReadOverride = async (kind: "post" | "comment", id: string, createdAt: Date): Promise<void> => {
    const currentlyUnread = isUnread(kind, id, createdAt);
    const target = { targetType: kind, targetId: id } as const;
    if (currentlyUnread) {
      setUnreadOverride(kind, id, false);
      await api.read.markRead(target);
    } else {
      setUnreadOverride(kind, id, true);
      await api.read.markUnread(target);
    }
    setOverrideTick((v) => v + 1);
  };

  const actions: ThreadActions = {
    get viewer() {
      return session.user();
    },
    get mentionCandidates() {
      return mentionCandidates();
    },
    async reply(parentCommentId, bodyMd) {
      const thread = threadRes();
      if (!thread) return;
      await api.thread.createComment({ postId: thread.post.id, parentCommentId, bodyMd });
      await refetch();
    },
    async editComment(id, bodyMd) {
      await api.thread.editComment({ id, bodyMd });
      await refetch();
    },
    async deleteComment(id) {
      await api.thread.deleteComment(id);
      await refetch();
    },
    async editPost(title, bodyMd) {
      const thread = threadRes();
      if (!thread) return;
      await api.thread.editPost({ id: thread.post.id, title, bodyMd });
      await refetch();
    },
    async deletePost() {
      const thread = threadRes();
      if (!thread) return;
      await api.thread.deletePost(thread.post.id);
      await refetch();
    },
    isUnread,
    isUnreadOverride,
    toggleReadOverride,
  };

  return (
    <section class="thread-page">
      <A href={`/b/${params.slug}`} class="back-link">
        ← back to {params.slug}
      </A>

      <Show when={threadRes()}>
        {(thread) => (
          <>
            <ContentCard
              targetType="post"
              id={thread().post.id}
              title={thread().post.title}
              bodyMd={thread().post.bodyMd}
              authorId={thread().post.authorId}
              origin={thread().post.origin}
              originRef={thread().post.originRef}
              createdAt={thread().post.createdAt}
              editedAt={thread().post.editedAt}
              deletedAt={thread().post.deletedAt}
              viewer={session.user()}
              ctx={actions}
              showReplyToggle={false}
              anchorId="post"
            />

            <div class="thread-controls">
              <h2 class="comments-heading">
                {thread().comments.length} {thread().comments.length === 1 ? "reply" : "replies"}
              </h2>
              <button type="button" class="view-mode-toggle" onClick={toggleMode}>
                {mode() === "tree" ? "Switch to flat (mailing-list) view" : "Switch to tree view"}
              </button>
            </div>

            <Show
              when={session.user()}
              fallback={
                <p class="page-status">
                  <A href="/login">Log in</A> to reply, endorse, or subscribe.
                </p>
              }
            >
              <Composer
                submitLabel="Post reply"
                placeholder="Reply to this post… (markdown supported)"
                mentionCandidates={mentionCandidates()}
                onSubmit={(values) => actions.reply(undefined, values.bodyMd)}
              />
            </Show>

            <CommentTree comments={thread().comments} mode={mode()} viewer={session.user()} ctx={actions} />
          </>
        )}
      </Show>
    </section>
  );
};

export default ThreadPage;
