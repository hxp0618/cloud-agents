// Visual reference only: our neutral composition imports the pinned upstream components.
// Install in an external Daytona checkout's ui/stories folder; never import into Admin Web.
import type { Meta, StoryObj } from "@storybook/react";
import {
  Activity,
  Box,
  CircleAlert,
  Container,
  HardDrive,
  MoreHorizontal,
  Plus,
  Search,
  Server,
  Settings,
  ShieldAlert,
} from "lucide-react";
import type { ReactNode } from "react";
import { Alert, AlertDescription, AlertTitle } from "../alert";
import { Badge } from "../badge";
import { Button } from "../button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "../empty";
import { Input } from "../input";
import { Label } from "../label";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "../sheet";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarSeparator,
  SidebarTrigger,
} from "../sidebar";
import { Skeleton } from "../skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableHeader,
  TableRow,
} from "../table";

const meta: Meta = { title: "Reference/Visual Baseline", parameters: { layout: "fullscreen" } };
export default meta;
type Story = StoryObj;

const nav = [
  { label: "Resources", icon: Container },
  { label: "Snapshots", icon: Box },
  { label: "Volumes", icon: HardDrive },
  { label: "Runners", icon: Server },
];

function Shell({ children }: { children: ReactNode }) {
  return (
    <SidebarProvider isBannerVisible={false} defaultOpen>
      <Sidebar isBannerVisible={false} collapsible="icon">
        <SidebarHeader>
          <div className="flex h-[46px] items-center justify-between gap-2 px-2 pt-2">
            <div className="flex items-center gap-2 overflow-hidden text-sm font-semibold">
              <span className="grid size-7 shrink-0 place-items-center rounded-md border">R</span>
              <span className="whitespace-nowrap group-data-[collapsible=icon]:hidden">
                Reference
              </span>
            </div>
            <SidebarTrigger className="p-2 [&_svg]:size-5" />
          </div>
        </SidebarHeader>
        <SidebarSeparator className="mx-0 w-full" />
        <SidebarContent className="pt-4">
          <SidebarMenu className="gap-2 px-2 pb-2">
            <SidebarMenuItem>
              <SidebarMenuButton variant="outline" className="bg-input/50">
                <Search className="size-4" />
                <span>Search</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {nav.map(({ label, icon: Icon }, index) => (
                  <SidebarMenuItem key={label}>
                    <SidebarMenuButton isActive={index === 0}>
                      <Icon className="size-4" />
                      <span>{label}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
          <SidebarSeparator />
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton>
                    <Activity className="size-4" />
                    <span>Operations</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                <SidebarMenuItem>
                  <SidebarMenuButton>
                    <Settings className="size-4" />
                    <span>Settings</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
        <SidebarFooter className="pb-4 text-xs text-muted-foreground">
          <span className="px-2 group-data-[collapsible=icon]:hidden">Fixed source fixture</span>
        </SidebarFooter>
      </Sidebar>
      <SidebarInset className="overflow-y-auto">
        <div className="flex min-h-svh flex-col">
          <header className="flex min-h-[55px] items-center gap-2 border-b bg-background px-4 py-[15px] sm:gap-4">
            <SidebarTrigger className="shrink-0 md:hidden [&_svg]:size-5" />
            <div className="flex-1" />
            <Button variant="ghost" size="sm">
              Docs
            </Button>
            <Button variant="outline" size="sm">
              Profile
            </Button>
          </header>
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

function Intro({ action = true }: { action?: boolean }) {
  return (
    <div className="mb-8 flex shrink-0 flex-col gap-1">
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-x-4 gap-y-3">
        <div className="flex flex-1 flex-col gap-1">
          <h1 className="text-2xl font-medium tracking-tight sm:text-3.5xl">Resources</h1>
          <div className="text-sm text-muted-foreground">
            Manage compute resources and lifecycle state.
          </div>
        </div>
        {action ? (
          <Button>
            <Plus className="size-4" />
            Create resource
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function Toolbar() {
  return (
    <div className="flex items-center gap-2">
      <Input
        className="max-w-sm"
        aria-label="Search resources"
        placeholder="Search by ID or name"
      />
      <Button variant="outline" size="icon" className="ml-auto" aria-label="More options">
        <MoreHorizontal />
      </Button>
    </div>
  );
}

function ResourceTable({ loading = false }: { loading?: boolean }) {
  const rows = [
    ["runner-a", "Docker", "Ready", "g4", "2 minutes ago"],
    ["runner-b", "Kubernetes", "Pending", "g2", "5 minutes ago"],
    ["runner-c", "SSH", "Unavailable", "g1", "1 hour ago"],
  ];
  return (
    <TableContainer>
      <Table>
        <TableHeader>
          <TableRow>
            {["Name", "Kind", "Status", "Generation", "Updated", ""].map((heading) => (
              <TableHead key={heading}>{heading}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {(loading ? Array.from({ length: 6 }, () => Array<string>(6).fill("")) : rows).map(
            (row, index) => (
              <TableRow key={index}>
                {row.map((cell, cellIndex) => (
                  <TableCell key={cellIndex}>
                    {loading ? (
                      <Skeleton className="h-4 w-10/12" />
                    ) : cellIndex === 2 ? (
                      <Badge
                        variant={index === 0 ? "success" : index === 1 ? "warning" : "destructive"}
                      >
                        {cell}
                      </Badge>
                    ) : (
                      cell
                    )}
                  </TableCell>
                ))}
                {!loading ? (
                  <TableCell>
                    <Button variant="ghost" size="icon">
                      <MoreHorizontal />
                    </Button>
                  </TableCell>
                ) : null}
              </TableRow>
            ),
          )}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

function ListPage({ loading = false, children }: { loading?: boolean; children?: ReactNode }) {
  return (
    <Shell>
      <main className="flex min-h-0 w-full flex-1 flex-col gap-4 overflow-auto p-4">
        <Intro />
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          <Toolbar />
          <ResourceTable loading={loading} />
        </div>
      </main>
      {children}
    </Shell>
  );
}

export const List: Story = { render: () => <ListPage /> };
export const Loading: Story = { render: () => <ListPage loading /> };

export const Detail: Story = {
  render: () => (
    <ListPage>
      <Sheet open>
        <SheetContent className="flex w-dvw flex-col gap-0 p-0 sm:w-[500px]">
          <SheetHeader className="flex-row items-center border-b p-4 px-5 text-left">
            <div>
              <SheetTitle>runner-a</SheetTitle>
              <SheetDescription>Docker resource</SheetDescription>
            </div>
          </SheetHeader>
          <div className="flex-1 space-y-6 overflow-auto p-5">
            <div className="flex items-center justify-between">
              <Badge variant="success">Ready</Badge>
              <span className="text-sm text-muted-foreground">Generation 4</span>
            </div>
            <dl className="grid grid-cols-[9rem_1fr] gap-x-4 gap-y-3 text-sm">
              <dt className="text-muted-foreground">Resource ID</dt>
              <dd>runner-a</dd>
              <dt className="text-muted-foreground">Architecture</dt>
              <dd>arm64</dd>
              <dt className="text-muted-foreground">Last heartbeat</dt>
              <dd>2 minutes ago</dd>
            </dl>
          </div>
          <SheetFooter className="border-t p-4 px-5">
            <Button variant="outline">Close</Button>
            <Button>Open operations</Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </ListPage>
  ),
};

export const CreateForm: Story = {
  render: () => (
    <ListPage>
      <Sheet open>
        <SheetContent className="flex w-dvw flex-col gap-0 p-0 sm:w-[500px]">
          <SheetHeader className="flex-row items-center border-b p-4 px-5 text-left">
            <div>
              <SheetTitle>Create resource</SheetTitle>
              <SheetDescription>Add a managed compute resource.</SheetDescription>
            </div>
          </SheetHeader>
          <div className="flex-1 space-y-5 overflow-auto p-5">
            <div className="space-y-2">
              <Label htmlFor="name">Name</Label>
              <Input id="name" placeholder="runner-primary" />
            </div>
            <div className="space-y-2">
              <Label htmlFor="endpoint">Endpoint</Label>
              <Input id="endpoint" placeholder="https://example.test" />
            </div>
            <div className="space-y-2">
              <Label htmlFor="reference">Credential reference</Label>
              <Input id="reference" placeholder="credential-primary" />
            </div>
          </div>
          <SheetFooter className="border-t p-4 px-5">
            <Button variant="outline">Cancel</Button>
            <Button>Create resource</Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </ListPage>
  ),
};

export const ConfirmDialog: Story = {
  render: () => (
    <ListPage>
      <Dialog open>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm lifecycle action</DialogTitle>
            <DialogDescription>This action affects one resource at generation 4.</DialogDescription>
          </DialogHeader>
          <Alert variant="warning">
            <CircleAlert />
            <AlertTitle>Review impact</AlertTitle>
            <AlertDescription>runner-a and workspace-runner-a will be affected.</AlertDescription>
          </Alert>
          <DialogFooter>
            <Button variant="outline">Cancel</Button>
            <Button variant="destructive">Confirm action</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </ListPage>
  ),
};

export const Dropdown: Story = {
  render: () => (
    <ListPage>
      <div className="fixed right-8 top-20">
        <DropdownMenu open>
          <DropdownMenuTrigger asChild>
            <Button variant="outline">Actions</Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem>View details</DropdownMenuItem>
            <DropdownMenuItem>Probe</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive">Cleanup</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </ListPage>
  ),
};

function StatePage({ variant }: { variant: "empty" | "error" | "denied" }) {
  const denied = variant === "denied";
  return (
    <Shell>
      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-4 p-4">
        <Intro action={false} />
        {variant === "error" ? (
          <Alert variant="destructive">
            <CircleAlert />
            <AlertTitle>Unable to load resources</AlertTitle>
            <AlertDescription>stable_error_code: upstream-unavailable</AlertDescription>
          </Alert>
        ) : (
          <Empty
            className="flex-none rounded-md border py-12"
            variant={denied ? "warning" : "neutral"}
          >
            <EmptyHeader>
              <EmptyMedia variant="icon">{denied ? <ShieldAlert /> : <Server />}</EmptyMedia>
              <EmptyTitle>
                {denied ? "You don't have access to this page" : "No resources found"}
              </EmptyTitle>
              <EmptyDescription>
                {denied
                  ? "Ask an administrator to grant the required access."
                  : "Create a resource to get started."}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              {denied ? (
                <Badge>resources read</Badge>
              ) : (
                <Button>
                  <Plus />
                  Create resource
                </Button>
              )}
            </EmptyContent>
          </Empty>
        )}
      </main>
    </Shell>
  );
}

export const EmptyState: Story = { render: () => <StatePage variant="empty" /> };
export const ErrorState: Story = { render: () => <StatePage variant="error" /> };
export const PermissionDenied: Story = { render: () => <StatePage variant="denied" /> };
