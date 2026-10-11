import type { ComponentType, SVGProps } from 'react'
import {
  ArrowDownTrayIcon,
  ArrowPathIcon,
  ArrowRightStartOnRectangleIcon,
  ArrowsRightLeftIcon,
  CheckCircleIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronUpIcon,
  ChevronRightIcon,
  ClipboardIcon,
  ClockIcon,
  CommandLineIcon,
  DocumentIcon,
  DocumentTextIcon,
  FolderIcon,
  HomeIcon,
  PencilSquareIcon,
  RectangleStackIcon,
  StarIcon,
  SwatchIcon,
  UserCircleIcon,
  WrenchScrewdriverIcon,
  XCircleIcon,
} from '@heroicons/react/24/outline'
// The only solid glyph in the set: a star toggle communicates its state by
// filling, so outline-vs-filled is the affordance rather than a style choice.
import { StarIcon as StarIconSolid } from '@heroicons/react/24/solid'

export type IconComponent = ComponentType<SVGProps<SVGSVGElement>>

// Heroicons' 24px outline set is drawn for 1.5 stroke at 24px. citric renders
// icons at 12–17px, where 1.5 goes wispy against the UI font, so everything is
// bumped to 1.8 in one place rather than per call site.
const STROKE = 1.8

/**
 * Renders a heroicon at citric's standard weight. `size` is in px and applies to
 * both axes. Any other SVG prop (style, className, aria-hidden) passes through.
 */
export function Icon({
  glyph: Glyph,
  size = 16,
  ...rest
}: { glyph: IconComponent; size?: number } & SVGProps<SVGSVGElement>) {
  return <Glyph width={size} height={size} strokeWidth={STROKE} {...rest} />
}

/**
 * One name per concept, so "which icon means a repository" is answered here and
 * not re-decided per page. Prefer adding to this map over importing from
 * @heroicons/react directly.
 */
export const Glyphs = {
  // navigation
  home: HomeIcon,
  changes: RectangleStackIcon,
  authored: PencilSquareIcon,
  assigned: CheckCircleIcon,
  repos: FolderIcon,
  starred: StarIcon,
  starredOn: StarIconSolid,
  admin: WrenchScrewdriverIcon,

  // user menu
  profile: UserCircleIcon,
  cli: CommandLineIcon,
  appearance: SwatchIcon,
  switchAccount: ArrowsRightLeftIcon,
  signOut: ArrowRightStartOnRectangleIcon,

  // repository browsing
  folder: FolderIcon,
  file: DocumentIcon,
  readme: DocumentTextIcon,
  download: ArrowDownTrayIcon,
  copy: ClipboardIcon,
  rerun: ArrowPathIcon,
  edit: PencilSquareIcon,

  // chevrons
  chevronLeft: ChevronLeftIcon,
  chevronRight: ChevronRightIcon,
  chevronDown: ChevronDownIcon,
  chevronUp: ChevronUpIcon,

  // build status. `running` is deliberately absent — it renders as <Spinner />,
  // which animates; heroicons' ArrowPath would need its own keyframes to match.
  pass: CheckCircleIcon,
  fail: XCircleIcon,
  queued: ClockIcon,
} satisfies Record<string, IconComponent>

/**
 * The heroicons clipboard as a raw markup string, for the one place that builds
 * HTML rather than React elements (renderMarkdown's code-block copy button).
 * Kept beside Glyphs.copy so the two can't drift apart.
 */
export const CLIPBOARD_SVG =
  `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="${STROKE}" aria-hidden="true">` +
  '<path stroke-linecap="round" stroke-linejoin="round" d="M15.666 3.888A2.25 2.25 0 0 0 13.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 0 1-.75.75H9a.75.75 0 0 1-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 0 1-2.25 2.25H6.75A2.25 2.25 0 0 1 4.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 0 1 1.927-.184"/>' +
  '</svg>'

/**
 * Git branching has no heroicons equivalent — the set carries no VCS glyphs — so
 * this stays hand-drawn. Kept here so it sits with the rest of the iconography
 * and inherits the same sizing convention.
 */
export function BranchIcon({ size = 12, ...rest }: { size?: number } & SVGProps<SVGSVGElement>) {
  return (
    <svg
      width={size} height={size} viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth={STROKE} strokeLinecap="round" strokeLinejoin="round"
      {...rest}
    >
      <line x1="6" y1="3" x2="6" y2="15" />
      <circle cx="18" cy="6" r="3" />
      <circle cx="6" cy="18" r="3" />
      <path d="M18 9a9 9 0 0 1-9 9" />
    </svg>
  )
}
