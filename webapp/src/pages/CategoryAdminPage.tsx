import { Navigate } from "@solidjs/router";
import { createEffect, createResource, createSignal, For, Show, type Component } from "solid-js";
import type { Board, Category } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { useSession } from "~/lib/session";

interface CategoryEditorProps {
  category: Category;
  boards: readonly Board[];
  onSaved: (category: Category) => void;
  onDeleted: (id: string) => void;
}

const BoardLimitEditor: Component<{ board: Board; onSaved: (board: Board) => void }> = (props) => {
  const [limit, setLimit] = createSignal(0);
  createEffect(() => setLimit(props.board.categoryLimit));
  const save = async (): Promise<void> => {
    const board = await api.board.updateBoard({ id: props.board.id, categoryLimit: limit() });
    props.onSaved(board);
  };
  return (
    <label>
      {props.board.title}
      <input
        type="number"
        min="0"
        max="100"
        value={limit()}
        onInput={(event) => setLimit(Number(event.currentTarget.value))}
      />
      <small>Zero allows any number.</small>
      <button type="button" onClick={() => void save()}>Save limit</button>
    </label>
  );
};

const CategoryEditor: Component<CategoryEditorProps> = (props) => {
  const [name, setName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [crossBoardPosting, setCrossBoardPosting] = createSignal(false);
  const [boardIds, setBoardIds] = createSignal<string[]>([]);
  const [busy, setBusy] = createSignal(false);

  createEffect(() => {
    setName(props.category.name);
    setDescription(props.category.description ?? "");
    setCrossBoardPosting(props.category.crossBoardPosting);
    setBoardIds([...props.category.boardIds]);
  });

  const toggleBoard = (id: string): void => {
    setBoardIds((current) => (current.includes(id) ? current.filter((item) => item !== id) : [...current, id]));
  };

  const save = async (): Promise<void> => {
    setBusy(true);
    try {
      const saved = await api.category.updateCategory({
        id: props.category.id,
        name: name().trim(),
        description: description(),
        crossBoardPosting: crossBoardPosting(),
        boardIds: boardIds(),
      });
      props.onSaved(saved);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (): Promise<void> => {
    const confirmed = window.confirm(
      `Delete ${props.category.name}? This removes it from every post. Posts with no other category become Uncategorized.`,
    );
    if (!confirmed) return;
    setBusy(true);
    try {
      await api.category.deleteCategory({ id: props.category.id, removeFromPosts: true });
      props.onDeleted(props.category.id);
    } finally {
      setBusy(false);
    }
  };

  return (
    <article class="settings-section category-admin-card">
      <p class="eyebrow">/{props.category.slug}</p>
      <label>
        Name
        <input value={name()} onInput={(event) => setName(event.currentTarget.value)} />
      </label>
      <label>
        Description
        <textarea value={description()} onInput={(event) => setDescription(event.currentTarget.value)} />
      </label>
      <label>
        <input
          type="checkbox"
          checked={crossBoardPosting()}
          onChange={(event) => setCrossBoardPosting(event.currentTarget.checked)}
        />
        Show one thread in all selected boards
      </label>
      <fieldset>
        <legend>Boards</legend>
        <For each={props.boards}>
          {(board) => (
            <label>
              <input type="checkbox" checked={boardIds().includes(board.id)} onChange={() => toggleBoard(board.id)} />
              {board.title}
            </label>
          )}
        </For>
      </fieldset>
      <div class="settings-actions">
        <button type="button" disabled={busy() || boardIds().length === 0 || name().trim() === ""} onClick={() => void save()}>
          Save
        </button>
        <button type="button" class="danger-button" disabled={busy()} onClick={() => void remove()}>
          Delete from all posts
        </button>
      </div>
    </article>
  );
};

const CategoryAdminPage: Component = () => {
  const session = useSession();
  const [boardsPage, { mutate: setBoardsPage }] = createResource(() => api.board.listBoards({ limit: 200 }));
  const [categoryPage, { mutate: setCategoryPage, refetch }] = createResource(
    () => api.category.listCategories({}).then((page) => page.categories),
  );
  const [slug, setSlug] = createSignal("");
  const [name, setName] = createSignal("");
  const [boardIds, setBoardIds] = createSignal<string[]>([]);

  const toggleNewBoard = (id: string): void => {
    setBoardIds((current) => (current.includes(id) ? current.filter((item) => item !== id) : [...current, id]));
  };

  const createCategory = async (event: Event): Promise<void> => {
    event.preventDefault();
    await api.category.createCategory({
      slug: slug().trim(),
      name: name().trim(),
      crossBoardPosting: false,
      boardIds: boardIds(),
    });
    setSlug("");
    setName("");
    setBoardIds([]);
    await refetch();
  };

  return (
    <Show when={session.user()?.roles.includes("admin")} fallback={<Navigate href="/" />}>
      <section class="settings-page">
        <header>
          <p class="eyebrow">Administration</p>
          <h1>Categories</h1>
          <p>Configure board topics and optional cross-board threads.</p>
        </header>

        <form class="settings-section" onSubmit={(event) => void createCategory(event)}>
          <h2>Create a category</h2>
          <label>
            Slug
            <input value={slug()} pattern="[a-z0-9]+(-[a-z0-9]+)*" onInput={(event) => setSlug(event.currentTarget.value)} />
          </label>
          <label>
            Name
            <input value={name()} onInput={(event) => setName(event.currentTarget.value)} />
          </label>
          <fieldset>
            <legend>Boards</legend>
            <For each={boardsPage()?.boards ?? []}>
              {(board) => (
                <label>
                  <input type="checkbox" checked={boardIds().includes(board.id)} onChange={() => toggleNewBoard(board.id)} />
                  {board.title}
                </label>
              )}
            </For>
          </fieldset>
          <button type="submit" disabled={slug().trim() === "" || name().trim() === "" || boardIds().length === 0}>
            Create category
          </button>
        </form>

        <section class="settings-section">
          <h2>Category limits</h2>
          <p>Set the maximum categories for a post in each board.</p>
          <For each={boardsPage()?.boards ?? []}>
            {(board) => (
              <BoardLimitEditor
                board={board}
                onSaved={(saved) =>
                  setBoardsPage((page) => page ? { ...page, boards: page.boards.map((item) => item.id === saved.id ? saved : item) } : page)
                }
              />
            )}
          </For>
        </section>

        <For each={categoryPage() ?? []}>
          {(category) => (
            <CategoryEditor
              category={category}
              boards={boardsPage()?.boards ?? []}
              onSaved={(saved) => setCategoryPage((items) => items?.map((item) => (item.id === saved.id ? saved : item)))}
              onDeleted={(id) => setCategoryPage((items) => items?.filter((item) => item.id !== id))}
            />
          )}
        </For>
      </section>
    </Show>
  );
};

export default CategoryAdminPage;
