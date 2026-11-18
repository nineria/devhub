/** The Ostler mark: a horseshoe, open end up, the way it is hung for luck. */
export function Horseshoe({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true">
      <path
        d="M18 10v18a14 14 0 0 0 28 0V10"
        fill="none"
        stroke="currentColor"
        strokeWidth="9"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <g fill="currentColor">
        <circle cx="18" cy="36" r="2.6" />
        <circle cx="24.5" cy="47" r="2.6" />
        <circle cx="46" cy="36" r="2.6" />
        <circle cx="39.5" cy="47" r="2.6" />
      </g>
    </svg>
  );
}

export function Brand() {
  return (
    <a href="#/" className="brand" aria-label="Ostler, all services">
      <Horseshoe />
      Ostler
    </a>
  );
}
