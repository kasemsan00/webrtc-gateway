import {
  HeadContent,
  Link,
  Outlet,
  Scripts,
  createRootRoute,
} from '@tanstack/react-router'
import { Toaster } from '@/components/ui/sonner'
import { KeycloakAuthProvider } from '@/features/auth/keycloak-provider'

import '../styles.css'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      {
        charSet: 'utf-8',
      },
      {
        name: 'viewport',
        content: 'width=device-width, initial-scale=1',
      },
      {
        title: 'WebRTC Gateway — SIP/WebRTC Console',
      },
      {
        name: 'description',
        content:
          'WebRTC Gateway test console for SIP and WebRTC calls, messaging, and trunk management.',
      },
    ],
  }),

  shellComponent: RootDocument,
  component: RootLayout,
  notFoundComponent: RootNotFound,
})

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
      <head>
        <HeadContent />
        <script
          dangerouslySetInnerHTML={{
            __html: `(function(){try{var r=localStorage.getItem('k2-theme');if(!r)return;var e=JSON.parse(r);if(e&&e.data==='light')document.documentElement.classList.remove('dark')}catch(x){}})()`,
          }}
        />
      </head>
      <body className="overflow-hidden" suppressHydrationWarning>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function RootLayout() {
  return (
    <KeycloakAuthProvider>
      <Outlet />
      <Toaster />
    </KeycloakAuthProvider>
  )
}

function RootNotFound() {
  return (
    <div className="flex h-screen flex-col items-center justify-center gap-3 bg-background px-6 text-foreground">
      <h1 className="text-xl font-semibold">Page not found</h1>
      <p className="text-sm text-muted-foreground">
        The requested route does not exist.
      </p>
      <Link
        to="/"
        className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-muted"
      >
        Back to home
      </Link>
    </div>
  )
}
