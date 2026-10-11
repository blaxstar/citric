// The selectable color palettes; their tokens live in src/palettes.css. The
// first is the default.
export interface Palette { id: string; name: string; dark: [string, string]; light: [string, string] }

// [background, accent] for each mode, for the picker swatches.
export const PALETTES: Palette[] = [
  { id: 'tangerine-dusk', name: 'Tangerine Dusk', dark: ['#0f0b08', '#ff9a2e'], light: ['#fdf7ef', '#c96a00'] },
  { id: 'navel', name: 'Navel', dark: ['#0e0c0a', '#ff8a24'], light: ['#faf6f1', '#c45a00'] },
  { id: 'meyer-lemon', name: 'Meyer Lemon', dark: ['#0c0c0b', '#f4d84e'], light: ['#fbfaf3', '#8c6d00'] },
  { id: 'blood-orange', name: 'Blood Orange', dark: ['#0e0a0a', '#ff5b3a'], light: ['#fbf5f3', '#c2321a'] },
  { id: 'grapefruit', name: 'Grapefruit', dark: ['#0e0b0b', '#ff7f6b'], light: ['#fdf6f5', '#c8412e'] },
  { id: 'key-lime', name: 'Key Lime', dark: ['#0a0c09', '#a6e35b'], light: ['#f6f9f2', '#4b7d12'] },
  { id: 'yuzu', name: 'Yuzu', dark: ['#0b0c09', '#d4e04a'], light: ['#f9faf1', '#6c7a00'] },
  { id: 'marmalade', name: 'Marmalade', dark: ['#100c08', '#eba73e'], light: ['#fcf6ec', '#a65f00'] },
  { id: 'zest', name: 'Zest', dark: ['#000000', '#ffe81a'], light: ['#ffffff', '#7a6600'] },
  { id: 'clementine-sea', name: 'Clementine & Sea', dark: ['#081115', '#ff9a3c'], light: ['#f4f8f9', '#c45c00'] },
]

export const DEFAULT_PALETTE = PALETTES[0].id
