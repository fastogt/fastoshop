// Inline SVG rather than an icon package: five glyphs do not justify a
// dependency, and these ship with the bundle instead of a font request.
const kProps = {
  width: 15,
  height: 15,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

export const IconBox = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
    <path d="m3.3 7 8.7 5 8.7-5M12 22V12" />
  </svg>
);

export const IconEye = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7z" />
    <circle cx="12" cy="12" r="3" />
  </svg>
);

export const IconEyeOff = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M9.9 4.2A10.9 10.9 0 0 1 12 4c6.4 0 10 7 10 7a18 18 0 0 1-3 4M6.6 6.6A18 18 0 0 0 2 11s3.6 7 10 7a10.9 10.9 0 0 0 4.2-.8" />
    <path d="m2 2 20 20M9.9 9.9a3 3 0 0 0 4.2 4.2" />
  </svg>
);

export const IconTag = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M20.6 13.4 12 22l-9-9V3h10l7.6 7.6a2 2 0 0 1 0 2.8z" />
    <circle cx="7.5" cy="7.5" r="1.2" />
  </svg>
);

export const IconTrash = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
    <path d="M10 11v6M14 11v6" />
  </svg>
);

export const IconCheck = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M20 6 9 17l-5-5" />
  </svg>
);

export const IconX = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M18 6 6 18M6 6l12 12" />
  </svg>
);

export const IconUndo = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M3 7v6h6" />
    <path d="M3.5 13a9 9 0 1 0 2.1-6.4L3 9" />
  </svg>
);

export const IconUpload = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
    <path d="M7 9l5-5 5 5M12 4v12" />
  </svg>
);

export const IconDownload = () => (
  <svg {...kProps} aria-hidden="true">
    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
    <path d="M7 11l5 5 5-5M12 16V4" />
  </svg>
);
