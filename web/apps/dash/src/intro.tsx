import { IssueIcon, MergeIcon, PipelineIcon, type AuthIntro } from "@git2/ui";

export const dashIntro: AuthIntro = {
  eyebrow: "Dash · for developers",
  headline: "Ship code from",
  highlight: "your own servers.",
  text: "Repositories, code review, issues and pipelines in one place your company controls.",
  features: [
    { icon: <MergeIcon />, title: "Merge requests" },
    { icon: <PipelineIcon />, title: "Pipelines" },
    { icon: <IssueIcon />, title: "Issues" },
  ],
};
