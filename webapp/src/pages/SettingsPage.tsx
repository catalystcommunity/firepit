// Keep subscription controls with the other user preferences so that the
// notifications page remains an inbox.
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
