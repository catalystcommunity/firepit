import { describe, expect, it } from "vitest";
import { methodToOp } from "./opNaming";

describe("methodToOp", () => {
  it.each([
    ["BeginLogin", "begin-login"],
    ["Logout", "logout"],
    ["Whoami", "whoami"],
    ["ListBoards", "list-boards"],
    ["GetBoard", "get-board"],
    ["CreateGithubMapping", "create-github-mapping"],
    ["ListMentionGrants", "list-mention-grants"],
    ["MarkNotificationRead", "mark-notification-read"],
  ])("kebab-cases %s -> %s", (method, expected) => {
    expect(methodToOp(method)).toBe(expected);
  });
});
