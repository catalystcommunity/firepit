// Component tests for the mention-policy/notify-on-endorse section (task
// C4, PLANDOC.md §7's accept criterion "settings mutations round-trip").
import { render, screen, waitFor } from "@solidjs/testing-library";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { UserSettings } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";

const { getSettings, updateSettings } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
}));

vi.mock("~/lib/api", () => ({
  api: { settings: { getSettings, updateSettings } },
}));

const { default: PreferencesSection } = await import("./PreferencesSection");

const DEFAULT_SETTINGS: UserSettings = {
  mentionPolicy: "subscribed",
  notifyOnEndorse: true,
  updatedAt: new Date("2026-01-01T00:00:00Z"),
};

beforeEach(() => {
  getSettings.mockReset().mockResolvedValue(DEFAULT_SETTINGS);
  updateSettings.mockReset();
});

describe("PreferencesSection", () => {
  it("changing the mention policy calls update-settings and reflects the server's response", async () => {
    updateSettings.mockResolvedValue({ ...DEFAULT_SETTINGS, mentionPolicy: "everyone" });
    const user = userEvent.setup();

    render(() => <PreferencesSection />);
    await waitFor(() => expect(screen.getByRole("radio", { name: /Only in places/ })).toBeChecked());

    await user.click(screen.getByRole("radio", { name: /Anyone, anywhere/ }));

    await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ mentionPolicy: "everyone" }));
    await waitFor(() => expect(screen.getByRole("radio", { name: /Anyone, anywhere/ })).toBeChecked());
  });

  it("rolls back the radio selection and shows an inline error when the update fails", async () => {
    updateSettings.mockRejectedValue(new FirepitServiceError({ code: ServiceErrorCode.Validation, message: "nope, try again" }));
    const user = userEvent.setup();

    render(() => <PreferencesSection />);
    await waitFor(() => expect(screen.getByRole("radio", { name: /Only in places/ })).toBeChecked());

    await user.click(screen.getByRole("radio", { name: /Never/ }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("nope, try again"));
    expect(screen.getByRole("radio", { name: /Only in places/ })).toBeChecked();
  });

  it("toggling notify-on-endorse round-trips optimistically", async () => {
    updateSettings.mockResolvedValue({ ...DEFAULT_SETTINGS, notifyOnEndorse: false });
    const user = userEvent.setup();

    render(() => <PreferencesSection />);
    await waitFor(() => expect(screen.getByRole("checkbox")).toBeChecked());

    await user.click(screen.getByRole("checkbox"));

    await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ notifyOnEndorse: false }));
    await waitFor(() => expect(screen.getByRole("checkbox")).not.toBeChecked());
  });
});
