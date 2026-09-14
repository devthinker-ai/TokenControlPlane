import { cn } from '@/lib/utils'

/** Geometric gate/route mark — three paths converging into one. */
export function BrandMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={cn('h-6 w-6 text-primary', className)}
      aria-hidden
    >
      <path
        d="M4 6.5L12 4l8 2.5"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M6 10v4.5c0 2.5 2.5 4.5 6 5.5 3.5-1 6-3 6-5.5V10"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M12 10v10"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
    </svg>
  )
}

export function BrandLockup({ className }: { className?: string }) {
  return (
    <div className={cn('flex items-center gap-2.5', className)}>
      <BrandMark />
      <span className="text-sm font-semibold tracking-tight text-foreground">
        TokenControlPlane
      </span>
    </div>
  )
}
