export function formatBalanceAmount(amount: number, digits = 2): string {
  const minimum = 10 ** -digits;
  if (amount !== 0 && Math.abs(amount) < minimum)
    return `${amount < 0 ? "−" : ""}<${minimum.toLocaleString(undefined, { maximumFractionDigits: digits })}`;
  return amount.toLocaleString(undefined, { maximumFractionDigits: digits });
}
