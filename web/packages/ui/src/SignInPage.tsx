import { useState, type FormEvent } from "react";
import { Link, Navigate, useLocation, useNavigate, type Location } from "react-router-dom";
import { AuthLayout, type AuthIntro, type OtherPlane } from "./AuthLayout";
import { useAuth } from "./AuthProvider";
import { Field, PasswordField } from "./Field";
import { LockIcon, MailIcon } from "./icons";
import { authErrorMessage } from "./messages";

interface SignInPageProps {
  // Shown in the heading: "Sign in to git2 Dash".
  plane: string;
  intro: AuthIntro;
  otherPlane?: OtherPlane;
  // Route of the registration page, if this app has one.
  registerPath?: string;
}

export function SignInPage({ plane, intro, otherPlane, registerPath }: SignInPageProps) {
  const { status, sessionExpired, signIn } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: Location } | null)?.from?.pathname ?? "/";

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  if (status === "signed-in" && !submitting) {
    return <Navigate to={from} replace />;
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await signIn(email, password);
      navigate(from, { replace: true });
    } catch (err) {
      setError(authErrorMessage(err));
      setSubmitting(false);
    }
  }

  return (
    <AuthLayout
      plane={plane}
      otherPlane={otherPlane}
      intro={intro}
      tabs={registerPath ? { signInPath: location.pathname, registerPath } : undefined}
      title={`Sign in to git2 ${plane}`}
      subtitle="Welcome back. Sign in with your email and password."
      footer={
        registerPath && (
          <>
            No account yet? <Link to={registerPath}>Create one</Link>
          </>
        )
      }
    >
      <form className="g2-form" onSubmit={onSubmit} noValidate>
        {sessionExpired && !error && (
          <p className="g2-notice" role="status">
            Your session has expired. Sign in again to continue.
          </p>
        )}
        {error && (
          <p className="g2-error" role="alert">
            {error}
          </p>
        )}
        <Field
          label="Email"
          icon={<MailIcon />}
          type="email"
          autoComplete="username"
          placeholder="ornek@eposta.com"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <PasswordField
          label="Password"
          icon={<LockIcon />}
          autoComplete="current-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <button type="submit" className="g2-button g2-primary g2-block" disabled={submitting || !email || !password}>
          {submitting ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </AuthLayout>
  );
}
