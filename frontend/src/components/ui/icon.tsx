import { cn } from '@/lib/utils'
import type { LucideIcon, LucideProps } from 'lucide-react'

/** Standardize stroke weight across the app (Phase 8). */
export function Icon({
  icon: Comp,
  className,
  ...props
}: { icon: LucideIcon } & LucideProps) {
  return <Comp className={cn('h-4 w-4', className)} strokeWidth={1.75} {...props} />
}
