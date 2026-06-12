import {
  Folder,
  HandCoins,
  Landmark,
  TrendingDown,
  Wallet,
} from "lucide-react";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "#/components/ui/sidebar.tsx";

export function NavMain() {
  return (
    <SidebarGroup>
      <SidebarGroupContent className="flex flex-col gap-2">
        <SidebarMenu>
          <SidebarMenuItem key="Budget">
            <SidebarMenuButton tooltip="Budget">
              <HandCoins />
              <span>Budget</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem key="Transactions">
            <SidebarMenuButton tooltip="Transactions">
              <Wallet />
              <span>Transactions</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem key="Accounts">
            <SidebarMenuButton tooltip="Accounts" asChild isActive>
              <a href="/accounts">
                <Landmark />
                <span>Accounts</span>
              </a>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem key="Categories">
            <SidebarMenuButton tooltip="Categories">
              <Folder />
              <span>Categories</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem key="Paydown">
            <SidebarMenuButton tooltip="Paydown">
              <TrendingDown />
              <span>Paydown</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}
