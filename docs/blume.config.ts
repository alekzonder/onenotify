import { defineConfig } from "blume";

// Every .md page under docs/ is part of the site, except internal material.
// Pages link to each other by site route ([messages](/messages)), never by .md
// path: see "Documentation site" in docs/development.md.
export default defineConfig({
  title: "onenotify",
  description:
    "Notifications for onecheck events and your own scripts: a queue per channel, delivery to Telegram, mail and ntfy, run by cron.",
  logo: { image: { light: "/favicon.svg", alt: "onenotify" } },
  deployment: {
    base: "/onenotify",
    site: "https://alekzonder.github.io",
  },
  github: {
    owner: "alekzonder",
    repo: "onenotify",
    dir: "docs",
  },
  content: {
    root: ".",
    exclude: ["superpowers/**", "node_modules/**", "dist/**", "**/_*", "**/.*"],
  },
  navigation: {
    sidebar: {
      display: "group",
    },
  },
});
