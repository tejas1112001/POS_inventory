// Motion tokens for the filter toolbar: seconds, cubic-bezier curves, blur
// in px and springs (visualDuration in seconds; bounce 0 = no overshoot).
export const motionTokens = {
  duration: { instant: 0.12, fast: 0.16, standard: 0.24 },
  ease: {
    enter: [0.16, 1, 0.3, 1],
    standard: [0.22, 1, 0.36, 1],
  },
  blur: { subtle: 2, soft: 4 },
  spring: {
    morph: { type: 'spring', visualDuration: 0.36, bounce: 0.1 },
    smooth: { type: 'spring', visualDuration: 0.3, bounce: 0 },
    snappy: { type: 'spring', visualDuration: 0.22, bounce: 0 },
  },
};
