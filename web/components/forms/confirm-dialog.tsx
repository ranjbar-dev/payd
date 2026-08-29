"use client";

import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";

import { TotpField } from "./totp-field";

export type ConfirmResult = { outcomeUnknown?: boolean } | void;

export function ConfirmDialog({
  open,
  title,
  apiText,
  confirmLabel,
  onClose,
  onConfirm,
  requiresTotp = false,
  ready = true,
  destructive = false,
  error,
}: Readonly<{
  open: boolean;
  title: string;
  apiText: ReactNode;
  confirmLabel: string;
  onClose: () => void;
  onConfirm: (totp: string) => Promise<ConfirmResult>;
  requiresTotp?: boolean;
  ready?: boolean;
  destructive?: boolean;
  error?: { code: string; details?: { totp_consumed?: boolean } } | null;
}>) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [totp, setTotp] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [outcomeUnknown, setOutcomeUnknown] = useState(false);

  useEffect(() => {
    if (open) dialog.current?.showModal();
    else dialog.current?.close();
  }, [open]);
  useEffect(() => {
    if (error?.details?.totp_consumed || error?.code === "unauthorized")
      setTotp("");
  }, [error]);
  useEffect(() => {
    if (!open) {
      setTotp("");
      setSubmitting(false);
      setOutcomeUnknown(false);
    }
  }, [open]);

  const submit = async () => {
    if (
      !ready ||
      submitting ||
      outcomeUnknown ||
      (requiresTotp && totp.length !== 6)
    )
      return;
    setSubmitting(true);
    try {
      const result = await onConfirm(totp);
      if (result?.outcomeUnknown) setOutcomeUnknown(true);
    } finally {
      setTotp("");
      setSubmitting(false);
    }
  };
  const consumed = error?.details?.totp_consumed;
  const disabled =
    !ready ||
    submitting ||
    outcomeUnknown ||
    (requiresTotp && totp.length !== 6);

  return (
    <dialog
      ref={dialog}
      className="card z-50 w-full max-w-lg p-0 text-ink backdrop:bg-black/60"
      onCancel={(event) => {
        event.preventDefault();
        if (!submitting) onClose();
      }}
    >
      <div className="border-b border-border-subtle px-5 py-4">
        <h2 className="font-semibold">{title}</h2>
      </div>
      <div className="space-y-4 px-5 py-4">
        <div className="border border-border-subtle bg-inset p-3 text-sm">
          {apiText}
        </div>
        {requiresTotp ? (
          <TotpField
            value={totp}
            onChange={setTotp}
            disabled={submitting || outcomeUnknown}
          />
        ) : null}
        {consumed ? (
          <p className="text-sm text-severity-warning" role="alert">
            That code has been used. Wait for the next code before correcting
            the request.
          </p>
        ) : null}
        {outcomeUnknown ? (
          <p className="text-sm text-severity-warning" role="alert">
            The outcome is unknown. Check the entity’s current state before
            taking any further action.
          </p>
        ) : null}
      </div>
      <div className="flex justify-end gap-2 border-t border-border-subtle px-5 py-4">
        <button
          type="button"
          className="btn btn-secondary"
          disabled={submitting}
          onClick={onClose}
        >
          Cancel
        </button>
        <button
          type="button"
          className={`btn ${destructive ? "btn-danger" : "btn-primary"}`}
          disabled={disabled}
          onClick={() => void submit()}
        >
          {submitting ? "Submitting…" : confirmLabel}
        </button>
      </div>
    </dialog>
  );
}
