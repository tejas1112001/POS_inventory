import * as React from "react"
import * as DialogPrimitive from "@radix-ui/react-dialog"
import { XIcon } from "lucide-react"

import { cn } from "@/lib/utils"

function Dialog({
  ...props
}) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />
}

function DialogTrigger({
  ...props
}) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />
}

function DialogPortal({
  ...props
}) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />
}

function DialogClose({
  ...props
}) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />
}

function DialogOverlay({
  className,
  ...props
}) {
  return (
    <DialogPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn(
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 z-50 bg-[#0b2a32]/55",
        className
      )}
      {...props}
    />
  )
}

// The close button. When the dialog has a DialogHeader the button sits in
// it (the header sticks, so the button stays in the corner while a long
// dialog scrolls and covers no field); otherwise in the dialog's corner.
const CloseSlot = React.createContext(null)

function CloseButton() {
  return (
    <DialogPrimitive.Close
      data-slot="dialog-close"
      className="ring-offset-background focus:ring-ring data-[state=open]:bg-accent data-[state=open]:text-muted-foreground absolute top-4 right-4 z-30 flex size-9 items-center justify-center rounded-lg opacity-70 transition-[opacity,background-color] hover:bg-accent hover:opacity-100 focus:ring-2 focus:ring-offset-2 focus:outline-hidden disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"
    >
      <XIcon />
      <span className="sr-only">Close</span>
    </DialogPrimitive.Close>
  )
}

// sheet: on phones (< 640 px) the dialog slides up from the bottom as a
// sheet; from 640 px it is a centred dialog.
const SHEET =
  "max-sm:top-auto max-sm:bottom-0 max-sm:left-0 max-sm:translate-x-0 max-sm:translate-y-0 max-sm:max-w-full max-sm:rounded-b-none max-sm:rounded-t-2xl max-sm:data-[state=open]:slide-in-from-bottom max-sm:data-[state=open]:zoom-in-100";

function DialogContent({
  className,
  children,
  showCloseButton = true,
  sheet = false,
  ...props
}) {
  const [closeInHeader, setCloseInHeader] = React.useState(false)
  return (
    <DialogPortal data-slot="dialog-portal">
      <DialogOverlay />
      <DialogPrimitive.Content
        data-slot="dialog-content"
        className={cn(
          "bg-card data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 fixed top-[50%] left-[50%] z-50 grid max-h-[calc(100dvh-2rem)] w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 overflow-y-auto overscroll-contain rounded-2xl border p-6 shadow-lg duration-200 sm:max-w-lg",
          sheet && SHEET,
          className
        )}
        {...props}
      >
        <CloseSlot.Provider value={showCloseButton ? setCloseInHeader : null}>{children}</CloseSlot.Provider>
        {showCloseButton && !closeInHeader && <CloseButton />}
      </DialogPrimitive.Content>
    </DialogPortal>
  )
}

// Header and footer stick to the edges of the scrolling dialog. The dialog
// has 24 px padding and sticky offsets count from inside it, hence -top-6
// and -bottom-6: nothing scrolls into view above the title or below the
// buttons.
function DialogHeader({ className, children, ...props }) {
  const register = React.useContext(CloseSlot)
  React.useLayoutEffect(() => {
    if (!register) return undefined
    register(true)
    return () => register(false)
  }, [register])
  return (
    <div
      data-slot="dialog-header"
      className={cn("sticky -top-6 z-10 -mx-6 -mt-6 flex flex-col gap-2 bg-card px-6 pt-6 pb-2 pr-12 text-left", className)}
      {...props}
    >
      {children}
      {register && <CloseButton />}
    </div>
  )
}

function DialogFooter({ className, ...props }) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn(
        "sticky -bottom-6 z-10 -mx-6 -mb-6 flex flex-row justify-end gap-2 border-t bg-card px-6 py-3 pb-[max(12px,env(safe-area-inset-bottom))] max-sm:[&>*]:flex-1",
        className
      )}
      {...props}
    />
  )
}

function DialogTitle({
  className,
  ...props
}) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn("text-lg leading-none font-bold text-navy", className)}
      {...props}
    />
  )
}

function DialogDescription({
  className,
  ...props
}) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn("text-muted-foreground text-sm", className)}
      {...props}
    />
  )
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
}
