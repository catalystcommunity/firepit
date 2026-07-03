import { describe, expect, it } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import App from "./App";

describe("App", () => {
  it("renders the Firepit placeholder shell", () => {
    render(() => <App />);
    expect(screen.getByText("Firepit")).toBeInTheDocument();
  });
});
