// /settings (task C4, PLANDOC.md §7): mention policy + notify-on-endorse,
// mention-permission grants, friend groups, and subscriptions management —
// four plain, well-labeled sections rather than a tabbed layout (design
// direction: calm and quiet, not a dashboard). Subscriptions management
// lives here rather than as a section on /notifications: this is already
// where "how I manage what I follow" lives for every other kind of
// preference, and it keeps /notifications focused purely on the inbox.
import type { Component } from "solid-js";
import FriendGroupsSection from "~/components/settings/FriendGroupsSection";
import MentionGrantsSection from "~/components/settings/MentionGrantsSection";
import PreferencesSection from "~/components/settings/PreferencesSection";
import SubscriptionsSection from "~/components/settings/SubscriptionsSection";

const SettingsPage: Component = () => (
  <div class="settings-page">
    <h2>Settings</h2>
    <PreferencesSection />
    <MentionGrantsSection />
    <FriendGroupsSection />
    <SubscriptionsSection />
  </div>
);

export default SettingsPage;
