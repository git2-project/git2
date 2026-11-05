import { useId, useState, type InputHTMLAttributes, type ReactNode } from "react";
import { EyeIcon, EyeOffIcon } from "./icons";

interface FieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "id"> {
  label: string;
  icon: ReactNode;
  hint?: string;
}

// Field is a labelled input with a leading icon.
export function Field({ label, icon, hint, ...input }: FieldProps) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  return (
    <div className="g2-field">
      <label htmlFor={id}>{label}</label>
      <div className="g2-input">
        <span className="g2-input-icon">{icon}</span>
        <input id={id} aria-describedby={hintId} {...input} />
      </div>
      {hint && (
        <p id={hintId} className="g2-field-hint">
          {hint}
        </p>
      )}
    </div>
  );
}

// PasswordField adds a show/hide toggle to Field.
export function PasswordField({ label, icon, hint, ...input }: FieldProps) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const [visible, setVisible] = useState(false);
  return (
    <div className="g2-field">
      <label htmlFor={id}>{label}</label>
      <div className="g2-input">
        <span className="g2-input-icon">{icon}</span>
        <input id={id} aria-describedby={hintId} {...input} type={visible ? "text" : "password"} />
        <button
          type="button"
          className="g2-input-action"
          aria-label={visible ? "Hide password" : "Show password"}
          aria-pressed={visible}
          onClick={() => setVisible((v) => !v)}
        >
          {visible ? <EyeOffIcon /> : <EyeIcon />}
        </button>
      </div>
      {hint && (
        <p id={hintId} className="g2-field-hint">
          {hint}
        </p>
      )}
    </div>
  );
}
