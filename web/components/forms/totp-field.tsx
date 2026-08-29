"use client";

export function TotpField({
  value,
  onChange,
  disabled = false,
  id = "payd-totp",
}: Readonly<{
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  id?: string;
}>) {
  return (
    <div>
      <label htmlFor={id} className="field mb-1">
        payd code
      </label>
      <input
        id={id}
        name="totp"
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        data-lpignore="true"
        data-1p-ignore="true"
        pattern="[0-9]*"
        maxLength={6}
        value={value}
        disabled={disabled}
        onChange={(event) =>
          onChange(event.currentTarget.value.replace(/\D/g, "").slice(0, 6))
        }
        onKeyDown={(event) => {
          if (event.key === "Enter") event.preventDefault();
        }}
        className="input w-36 font-mono tracking-[0.25em]"
        aria-describedby={`${id}-help`}
      />
      <p id={`${id}-help`} className="mt-1 text-xs text-ink-faint">
        Six digits. This single-use payd code is not your dashboard code.
      </p>
    </div>
  );
}
