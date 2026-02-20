"use client";

export function ThemedLogo() {
  return (
    <div className="relative flex items-center justify-center size-8">
      <svg
        viewBox="0 0 32 32"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className="size-8"
        aria-hidden="true"
      >
        {/* Key symbol */}
        <rect
          x="2"
          y="2"
          width="28"
          height="28"
          rx="6"
          className="fill-amber-500 dark:fill-amber-400"
        />
        <circle cx="12" cy="14" r="4" className="fill-white" strokeWidth="0" />
        <circle
          cx="12"
          cy="14"
          r="2"
          className="fill-amber-500 dark:fill-amber-400"
        />
        <rect
          x="16"
          y="13"
          width="8"
          height="2"
          rx="1"
          className="fill-white"
        />
        <rect
          x="21"
          y="15"
          width="2"
          height="3"
          rx="0.5"
          className="fill-white"
        />
        <rect
          x="24"
          y="15"
          width="2"
          height="2"
          rx="0.5"
          className="fill-white/70"
        />
      </svg>
    </div>
  );
}
