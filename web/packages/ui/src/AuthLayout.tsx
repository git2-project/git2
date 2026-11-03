import type { CSSProperties, ReactNode } from "react";
import { NavLink } from "react-router-dom";
import { BranchBackdrop } from "./Backdrop";
import { Logo } from "./Logo";
import { ThemeToggle } from "./ThemeToggle";

export interface AuthFeature {
  icon: ReactNode;
  title: string;
}

// AuthIntro is the plane's pitch next to the sign-in and registration forms.
// Each app supplies its own. The headline reads as one sentence:
// `${headline} ${highlight}`, with the highlight in the accent colour.
export interface AuthIntro {
  eyebrow: string;
  headline: string;
  highlight: string;
  text: string;
  features: AuthFeature[];
}

export interface OtherPlane {
  label: string;
  href: string;
}

interface AuthLayoutProps {
  plane: string;
  // Link to the other plane's sign-in, top right. A full page load: Dash and
  // Admin are separate apps.
  otherPlane?: OtherPlane;
  intro: AuthIntro;
  // Sign-in / create-account switch, shown when the app has registration.
  tabs?: { signInPath: string; registerPath: string };
  title: string;
  subtitle?: string;
  children: ReactNode;
  footer?: ReactNode;
}

// Staggers the entrance animation of consecutive blocks.
const rise = (step: number) => ({ "--g2-delay": `${step * 70}ms` }) as CSSProperties;

export function AuthLayout({ plane, otherPlane, intro, tabs, title, subtitle, children, footer }: AuthLayoutProps) {
  return (
    <div className="g2-auth">
      <BranchBackdrop />
      <header className="g2-auth-header">
        <Logo plane={plane} />
        <div className="g2-header-actions">
          <ThemeToggle />
          {otherPlane && (
            <a className="g2-button g2-header-link" href={otherPlane.href}>
              {otherPlane.label}
            </a>
          )}
        </div>
      </header>

      <main className="g2-auth-main">
        <section className="g2-auth-hero" aria-label={`About git2 ${plane}`}>
          <p className="g2-eyebrow g2-rise" style={rise(0)}>
            {intro.eyebrow}
          </p>
          <p className="g2-hero-title g2-rise" style={rise(1)}>
            {intro.headline} <span className="g2-hero-highlight">{intro.highlight}</span>
          </p>
          <p className="g2-hero-text g2-rise" style={rise(2)}>
            {intro.text}
          </p>
          <ul className="g2-chips g2-rise" style={rise(3)}>
            {intro.features.map((f) => (
              <li key={f.title} className="g2-chip">
                {f.icon}
                {f.title}
              </li>
            ))}
          </ul>
        </section>

        <section className="g2-auth-card g2-rise" style={rise(2)}>
          {tabs && (
            <nav className="g2-tabs" aria-label="Account">
              <NavLink to={tabs.signInPath} replace>
                Sign in
              </NavLink>
              <NavLink to={tabs.registerPath} replace>
                Create account
              </NavLink>
            </nav>
          )}
          <div className="g2-auth-heading">
            <h1>{title}</h1>
            {subtitle && <p className="g2-muted">{subtitle}</p>}
          </div>
          {children}
          {footer && <div className="g2-auth-footer">{footer}</div>}
        </section>
      </main>

      <footer className="g2-auth-foot">Self-hosted · Open source · Your code stays on your servers</footer>
    </div>
  );
}
