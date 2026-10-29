import { ServerIcon, ShieldIcon, UsersIcon, type AuthIntro } from "@git2/ui";

export const adminIntro: AuthIntro = {
  eyebrow: "Admin · for IT",
  headline: "Your git2 instance,",
  highlight: "fully under control.",
  text: "People, single sign-on, runners and backups, managed without touching the server.",
  features: [
    { icon: <UsersIcon />, title: "Users and SSO" },
    { icon: <ServerIcon />, title: "Runners" },
    { icon: <ShieldIcon />, title: "Backups and audit log" },
  ],
};
