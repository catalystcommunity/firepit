// App shell + routing (task C1, PLANDOC.md §7). Routes: "/", "/login",
// "/auth/callback" are real; "/b/:slug", "/b/:slug/p/:postId",
// "/notifications", "/settings" are placeholders C2-C4 replace.
import { Route, Router } from "@solidjs/router";
import type { Component } from "solid-js";
import AppShell from "~/components/AppShell";
import { SessionProvider } from "~/lib/session";
import AuthCallback from "~/pages/AuthCallback";
import BoardPage from "~/pages/BoardPage";
import Home from "~/pages/Home";
import Login from "~/pages/Login";
import NotificationsPage from "~/pages/NotificationsPage";
import SettingsPage from "~/pages/SettingsPage";
import ThreadPage from "~/pages/ThreadPage";

const App: Component = () => (
  <SessionProvider>
    <Router root={AppShell}>
      <Route path="/" component={Home} />
      <Route path="/login" component={Login} />
      <Route path="/auth/callback" component={AuthCallback} />
      <Route path="/b/:slug" component={BoardPage} />
      <Route path="/b/:slug/p/:postId" component={ThreadPage} />
      <Route path="/notifications" component={NotificationsPage} />
      <Route path="/settings" component={SettingsPage} />
    </Router>
  </SessionProvider>
);

export default App;
