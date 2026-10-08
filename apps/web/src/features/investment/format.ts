export function localTime(instant: string): string {
  const date = new Date(instant);
  if (Number.isNaN(date.getTime())) return instant;
  const options: Intl.DateTimeFormatOptions = {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZoneName: "short",
  };
  if (date.getFullYear() !== new Date().getFullYear()) options.year = "numeric";
  return date.toLocaleString("zh-CN", options);
}

export function groupMoney(text: string): string {
  if (!/^-?[0-9]+\.[0-9]{2}$/.test(text)) return text;
  const negative = text.startsWith("-");
  const body = negative ? text.slice(1) : text;
  const [integer, fraction] = body.split(".");
  return (
    (negative ? "-" : "") +
    integer.replace(/\B(?=(\d{3})+(?!\d))/g, ",") +
    "." +
    fraction
  );
}
