import { cn } from '@/lib/utils'
import { HTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from 'react'
import { EmptyState } from '@/components/EmptyState'
import { Skeleton } from '@/components/ui/skeleton'
import type { LucideIcon } from 'lucide-react'

export function Table({ className, ...props }: HTMLAttributes<HTMLTableElement>) {
  return (
    <div className="w-full overflow-x-auto">
      <table className={cn('w-full caption-bottom text-sm', className)} {...props} />
    </div>
  )
}

export function TableHeader({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return <thead className={cn('[&_tr]:border-b', className)} {...props} />
}

export function TableBody({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return <tbody className={cn('[&_tr:last-child]:border-0', className)} {...props} />
}

export function TableRow({ className, ...props }: HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr
      className={cn(
        'group h-11 border-b border-border/60 transition-colors hover:bg-muted/40',
        className,
      )}
      {...props}
    />
  )
}

export function TableHead({ className, ...props }: ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th
      className={cn(
        'h-10 px-4 text-left align-middle text-xs font-medium text-muted-foreground',
        className,
      )}
      {...props}
    />
  )
}

export function TableCell({ className, ...props }: TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn('px-4 py-0 align-middle', className)} {...props} />
}

export function TableEmpty({
  colSpan,
  icon,
  title,
  description,
  actionLabel,
  onAction,
}: {
  colSpan: number
  icon: LucideIcon
  title: string
  description?: string
  actionLabel?: string
  onAction?: () => void
}) {
  return (
    <tr>
      <td colSpan={colSpan} className="p-0">
        <EmptyState
          icon={icon}
          title={title}
          description={description}
          actionLabel={actionLabel}
          onAction={onAction}
          className="border-0"
        />
      </td>
    </tr>
  )
}

export function TableLoading({ colSpan, rows = 3 }: { colSpan: number; rows?: number }) {
  return (
    <>
      {Array.from({ length: rows }).map((_, i) => (
        <tr key={i} className="h-11 border-b border-border/60">
          <td colSpan={colSpan} className="px-4">
            <Skeleton className="h-4 w-full max-w-md" />
          </td>
        </tr>
      ))}
    </>
  )
}
