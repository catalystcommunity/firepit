// Router smoke test against the in-memory transport configured in .env.test.
import { render, screen, waitFor, within } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it } from "vitest";
import App from "./App";

beforeEach(() => {
  window.history.pushState({}, "", "/");
});

describe("App", () => {
  it("renders the shell at / with the brand, boards rail, and home content", async () => {
    render(() => <App />);

    expect(screen.getByRole("link", { name: "Firepit" })).toBeInTheDocument();
    expect(screen.getByText("Boards")).toBeInTheDocument();
    expect(screen.getByText(/Welcome to Firepit\.|Welcome back,/)).toBeInTheDocument();

    // The board rail resolves from the mock transport's fixture boards.
    // Scoped to the rail itself (`getByRole("navigation", ...)`) — task C2's
    // real board index on "/" also lists every board by name, so an
    // unscoped `getByText` would find both and fail on ambiguity.
    const rail = screen.getByRole("navigation", { name: "Boards" });
    await waitFor(() => expect(within(rail).getByText("Firepit Meta")).toBeInTheDocument());
    expect(within(rail).getByText("Announcements")).toBeInTheDocument();
  });
});
