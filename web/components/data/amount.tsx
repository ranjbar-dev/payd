import { cn } from "@/lib/utils";

type AmountVariant = "default" | "compact" | "usd-snapshot" | "usd-live";
type AmountProps =
  | {
      value: string;
      asset: string;
      variant?: AmountVariant;
      className?: string;
      unavailable?: false;
    }
  | {
      asset: string;
      variant?: AmountVariant;
      className?: string;
      unavailable: true;
      value?: string;
    };

export function Amount(props: Readonly<AmountProps>) {
  if (props.unavailable) {
    return (
      <span
        title="No fresh price was available"
        className={cn("text-ink-faint", props.className)}
      >
        —
      </span>
    );
  }

  const { value, asset, variant = "default", className } = props;

  const source =
    variant === "usd-snapshot"
      ? "USD snapshot"
      : variant === "usd-live"
        ? "USD live"
        : null;

  return (
    <span
      className={cn(
        "font-mono tabular-nums text-ink",
        variant === "compact" && "text-xs",
        className,
      )}
      data-financial-value
    >
      {value} {asset}
      {source ? (
        <span className="ml-1 font-sans text-xs text-ink-faint">
          ({source})
        </span>
      ) : null}
    </span>
  );
}
