import { Link, useNavigate } from "@tanstack/react-router";
import { ThemeProvider, useTheme } from "next-themes";
import type { ReactNode } from "react";
import { deleteUserPlugin } from "@/lib/auth/delete-user-plugin";
import { passkeyPlugin } from "@/lib/auth/passkey-plugin";
import { themePlugin } from "@/lib/auth/theme-plugin";
import { authClient } from "@/lib/auth-client";
import { AuthProvider } from "./auth/auth-provider";
import { Toaster } from "./ui/sonner";

export function Providers({ children }: { children: ReactNode }) {
  const navigate = useNavigate();

  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      disableTransitionOnChange
    >
      <AuthProvider
        authClient={authClient}
        redirectTo="/settings/account"
        socialProviders={[]}
        navigate={navigate}
        plugins={[
          themePlugin({ useTheme }),
          deleteUserPlugin(),
          passkeyPlugin(),
        ]}
        Link={Link}
      >
        {children}

        <Toaster />
      </AuthProvider>
    </ThemeProvider>
  );
}
