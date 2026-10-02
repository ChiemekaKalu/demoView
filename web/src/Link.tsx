import type { AccessibleAnchorHTMLAttributes, TargetedMouseEvent } from 'preact'
import { navigate } from './router'

type LinkProps = AccessibleAnchorHTMLAttributes<HTMLAnchorElement> & { href: string }

// An <a> that navigates in-app on a plain left click.
export function Link(props: LinkProps) {
  const onClick = (e: TargetedMouseEvent<HTMLAnchorElement>) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    if (props.target && props.target !== '_self') return
    e.preventDefault()
    navigate(props.href)
  }
  return <a {...props} onClick={onClick} />
}
