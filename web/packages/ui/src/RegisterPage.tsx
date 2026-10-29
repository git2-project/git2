import { register } from "@git2/api";
import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { AuthLayout, type AuthIntro, type OtherPlane } from "./AuthLayout";
import { useAuth } from "./AuthProvider";
import { Field, PasswordField } from "./Field";
import { LockIcon, MailIcon, UserIcon } from "./icons";
import { authErrorMessage } from "./messages";

// Same limits as auth-svc: bcrypt ignores everything past 72 bytes.
const MIN_PASSWORD = 8;
const MAX_PASSWORD_BYTES = 72;

function passwordProblem(password: string): string | null {
  if (password.length < MIN_PASSWORD) return `Password must be at least ${MIN_PASSWORD} characters.`;
  if (new TextEncoder().encode(password).length > MAX_PASSWORD_BYTES) {
    return `Password must be at most ${MAX_PASSWORD_BYTES} bytes.`;
  }
  return null;
}

interface RegisterPageProps {
  plane: string;
  intro: AuthIntro;
  otherPlane?: OtherPlane;
  signInPath?: string;
}

export function RegisterPage({ plane, intro, otherPlane, signInPath = "/sign-in" }: RegisterPageProps) {
  const { signIn } = useAuth();
  const navigate = useNavigate();

  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const problem = passwordProblem(password);
    if (problem) {
      setError(problem);
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      await register(email, username, password);
      await signIn(email, password);
      navigate("/", { replace: true });
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
      tabs={{ signInPath, registerPath: "/register" }}
      title="Create your git2 account"
      subtitle="Your username is how teammates mention you and where your repositories live."
      footer={
        <>
          Already have an account? <Link to={signInPath}>Sign in</Link>
        </>
      }
    >
      <form className="g2-form" onSubmit={onSubmit} noValidate>
        {error && (
          <p className="g2-error" role="alert">
            {error}
          </p>
        )}
        <Field
          label="Email"
          icon={<MailIcon />}
          type="email"
          autoComplete="email"
          placeholder="ornek@eposta.com"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <Field
          label="Username"
          icon={<UserIcon />}
          autoComplete="username"
          required
          value={username}
          onChange={(e) => setUsername(e.target.value)}
        />
        <PasswordField
          label="Password"
          icon={<LockIcon />}
          autoComplete="new-password"
          hint={`${MIN_PASSWORD}–${MAX_PASSWORD_BYTES} characters.`}
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <button
          type="submit"
          className="g2-button g2-primary g2-block"
          disabled={submitting || !email || !username || !password}
        >
          {submitting ? "Creating account…" : "Create account"}
        </button>
      </form>
    </AuthLayout>
  );
}
