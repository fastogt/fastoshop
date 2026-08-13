// Prices travel in kopecks and are shown in rubles, everywhere in the admin.
export const toRubles = (minor: number) => (minor / 100).toFixed(2);
export const toMinor = (rubles: string) =>
  Math.round(Number(rubles.replace(",", ".")) * 100);
