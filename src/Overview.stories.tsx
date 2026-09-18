import * as React from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { expect, userEvent, within } from "storybook/test";
import { Button } from "./components/Button";
import { Badge } from "./components/Badge";
import { Input } from "./components/Input";
import { Textarea } from "./components/Textarea";
import { Checkbox } from "./components/Checkbox";
import { Switch } from "./components/Switch";
import { Slider } from "./components/Slider";
import { Progress } from "./components/Progress";
import { Skeleton } from "./components/Skeleton";
import { Spinner } from "./components/Spinner";
import { Alert } from "./components/Alert";
import { Avatar, AvatarGroup } from "./components/Avatar";
import { Heading } from "./components/Heading";
import { Text } from "./components/Text";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "./components/Card";
import { Tabs } from "./components/Tabs";
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "./components/Accordion";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "./components/Select";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "./components/Table";
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogClose,
} from "./components/Dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "./components/DropdownMenu";
import { Popover, PopoverTrigger, PopoverContent } from "./components/Popover";
import { TooltipProvider } from "./components/Tooltip";
import { Toaster, toast } from "./components/Toast";
import {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "./components/Breadcrumb";
import { Pagination, PaginationItem, PaginationEllipsis } from "./components/Pagination";
import {
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "./components/Command";
import { DatePicker } from "./components/Calendar";
import {
  SidebarProvider,
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarTrigger,
  SidebarNav,
  SidebarNavItem,
  AppShell,
} from "./components/Sidebar";
import { StatCard } from "./components/StatCard";
import { StatusDot } from "./components/StatusDot";
import { LineChart } from "./components/LineChart";
import { BarChart } from "./components/BarChart";
import { DonutChart } from "./components/DonutChart";
import { BarList } from "./components/BarList";
import { Heatmap } from "./components/Heatmap";
import { Gauge } from "./components/Gauge";
import { UptimeBar } from "./components/UptimeBar";
import { LogStream } from "./components/LogStream";
import { Timeline } from "./components/Timeline";
import { TraceWaterfall } from "./components/TraceWaterfall";
import { TimeRange } from "./components/TimeRange";
import { formatDuration, formatPercent } from "./lib/chart";
import { DARK_PALETTE, LIGHT_PALETTE } from "./tokens/palettes";

const meta: Meta = {
  title: "Forsight/Overview",
  parameters: {
    layout: "fullscreen",
    // This story is a full page with a real <main> landmark, so re-enable the
    // `region` rule the test runner turns off for isolated component stories.
    a11y: { options: { rules: { region: { enabled: true } } } },
  },
};
export default meta;
type Story = StoryObj;

function Section({
  id,
  title,
  children,
}: {
  id: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section aria-labelledby={id} className="flex flex-col gap-5">
      <Heading id={id} as="h2" size="sm" className="text-fg-secondary">
        {title}
      </Heading>
      {children}
    </section>
  );
}

function Row({ children }: { children: React.ReactNode }) {
  return <div className="flex flex-wrap items-center gap-3">{children}</div>;
}

const swatchGroups: { label: string; keys: string[] }[] = [
  { label: "Ink", keys: ["bg", "surface", "surface2"] },
  {
    label: "Accent — Rapids Teal",
    keys: ["accent", "accentHover", "accentActive", "accentSubtle"],
  },
  { label: "Spark — Amber", keys: ["spark", "sparkHover", "sparkSubtle"] },
  { label: "Semantic", keys: ["danger", "success", "warning"] },
];

const deployments = [
  { id: "dpl_a1", branch: "main", status: "Ready", env: "Production", when: "2m ago" },
  { id: "dpl_b2", branch: "feat/onboarding", status: "Building", env: "Preview", when: "just now" },
  { id: "dpl_c3", branch: "fix/nav-focus", status: "Error", env: "Preview", when: "14m ago" },
];

const statusVariant: Record<string, "success" | "warning" | "danger"> = {
  Ready: "success",
  Building: "warning",
  Error: "danger",
};

function KitchenSink() {
  const [email, setEmail] = React.useState("marc@forscorp.com");
  const [plan, setPlan] = React.useState("rapids");
  const [notify, setNotify] = React.useState(true);
  const [previews, setPreviews] = React.useState(true);
  const [concurrency, setConcurrency] = React.useState(40);
  const [saving, setSaving] = React.useState(false);
  const [page, setPage] = React.useState(1);
  const [commandOpen, setCommandOpen] = React.useState(false);
  const [deployDate, setDeployDate] = React.useState<Date | undefined>();

  const emailInvalid = !email.includes("@");

  function runCommand(action: () => void) {
    setCommandOpen(false);
    action();
  }

  const TOTAL_PAGES = 12;
  const pageWindow = React.useMemo(() => {
    const set = new Set([1, TOTAL_PAGES, page, page - 1, page + 1]);
    return [...set].filter((n) => n >= 1 && n <= TOTAL_PAGES).sort((a, b) => a - b);
  }, [page]);

  function handleSave() {
    if (emailInvalid) return;
    setSaving(true);
    window.setTimeout(() => {
      setSaving(false);
      toast({
        variant: "success",
        title: "Settings saved",
        description: `${plan} plan · previews ${previews ? "on" : "off"} · limit ${concurrency}`,
      });
    }, 800);
  }

  return (
    <TooltipProvider delayDuration={200}>
      <main className="min-h-screen bg-ink-bg p-4 sm:p-8 lg:p-10">
        <div className="mx-auto flex max-w-5xl flex-col gap-14 sm:gap-16">
          <header className="flex flex-wrap items-start justify-between gap-4">
            <div className="flex flex-col gap-2">
              <Heading as="h1" size="2xl">
                Forsight
              </Heading>
              <Text tone="secondary">
                Rapids Teal on near-black ink · Space Grotesk + Inter · 30 components, dark &amp;
                light, responsive, WCAG&nbsp;AA
              </Text>
            </div>
            <Button variant="secondary" onClick={() => setCommandOpen(true)}>
              Quick actions…
            </Button>
          </header>

          <Section id="ov-color" title="Color tokens (dark palette shown)">
            <div className="flex flex-col gap-4">
              {swatchGroups.map((group) => (
                <div key={group.label} className="flex flex-col gap-1.5">
                  <Text size="sm" tone="muted">
                    {group.label}
                  </Text>
                  <div className="flex flex-wrap gap-2">
                    {group.keys.map((k) => {
                      const hex = (DARK_PALETTE as unknown as Record<string, string>)[k];
                      return (
                        <div key={k} className="flex flex-col gap-1">
                          {/* border-2 in the mid-tone fg-muted stays visible on both
                              near-black and near-white pages, so a swatch whose colour
                              is close to the page background still shows its extent. */}
                          <div
                            className="h-12 w-20 rounded-md border-2 border-fg-muted shadow-sm sm:w-24"
                            style={{ background: hex }}
                            title={`${k} · ${hex}`}
                          />
                          <Text size="xs" tone="muted" className="font-mono">
                            {k}
                          </Text>
                        </div>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          </Section>

          <Section id="ov-type" title="Typography">
            <div className="flex flex-col gap-1">
              <p className="font-heading text-3xl font-semibold text-fg sm:text-4xl">
                Build faster. Own it forever.
              </p>
              <p className="font-heading text-xl font-semibold text-fg">
                Section heading — Space Grotesk
              </p>
              <Text>
                Body copy is Inter at a comfortable reading size. The quick brown fox jumps over the
                lazy dog.
              </Text>
              <Text tone="secondary">Secondary text for supporting detail.</Text>
              <Text tone="muted" size="sm">
                Muted caption text.
              </Text>
            </div>
          </Section>

          <Section id="ov-buttons" title="Buttons">
            <Row>
              <Button variant="primary">Primary</Button>
              <Button variant="secondary">Secondary</Button>
              <Button variant="spark">Spark</Button>
              <Button variant="ghost">Ghost</Button>
              <Button variant="danger">Danger</Button>
              <Button loading>Loading</Button>
              <Button disabled>Disabled</Button>
            </Row>
            <Row>
              <Button size="sm">Small</Button>
              <Button size="md">Medium</Button>
              <Button size="lg">Large</Button>
            </Row>
          </Section>

          <Section id="ov-badges" title="Badges">
            <Row>
              <Badge>Draft</Badge>
              <Badge variant="accent">New</Badge>
              <Badge variant="spark">Featured</Badge>
              <Badge variant="success">Shipped</Badge>
              <Badge variant="warning">Pending review</Badge>
              <Badge variant="danger">Failed</Badge>
            </Row>
          </Section>

          <Section id="ov-controls" title="Interactive settings form">
            <form
              className="grid gap-5 [grid-template-columns:repeat(auto-fit,minmax(18rem,1fr))]"
              onSubmit={(e) => {
                e.preventDefault();
                handleSave();
              }}
            >
              <div className="flex flex-col gap-1.5">
                <label htmlFor="ov-email" className="font-sans text-sm font-medium text-fg">
                  Billing email
                </label>
                <Input
                  id="ov-email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  invalid={emailInvalid}
                  hint={emailInvalid ? "Enter a valid email address." : undefined}
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label htmlFor="ov-plan" className="font-sans text-sm font-medium text-fg">
                  Plan
                </label>
                <Select value={plan} onValueChange={setPlan}>
                  <SelectTrigger id="ov-plan">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="starter">Starter</SelectItem>
                    <SelectItem value="rapids">Rapids</SelectItem>
                    <SelectItem value="enterprise">Enterprise</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="flex flex-col gap-1.5">
                <label htmlFor="ov-deploy-date" className="font-sans text-sm font-medium text-fg">
                  Scheduled deploy date
                </label>
                <DatePicker
                  id="ov-deploy-date"
                  value={deployDate}
                  onValueChange={setDeployDate}
                  placeholder="No date scheduled"
                  className="w-full"
                />
              </div>

              <div className="flex flex-col gap-1.5 [grid-column:1/-1]">
                <label htmlFor="ov-notes" className="font-sans text-sm font-medium text-fg">
                  Deployment notes
                </label>
                <Textarea id="ov-notes" placeholder="Optional context for your team…" rows={2} />
              </div>

              <div className="flex flex-col gap-3">
                <label className="flex items-center gap-2">
                  <Checkbox checked={notify} onCheckedChange={(v) => setNotify(v === true)} />
                  <Text size="sm">Email me about deploy failures</Text>
                </label>
                <label className="flex items-center gap-2">
                  <Switch checked={previews} onCheckedChange={setPreviews} />
                  <Text size="sm">Enable preview deployments</Text>
                </label>
              </div>

              <div className="flex flex-col gap-2">
                <label htmlFor="ov-conc" className="font-sans text-sm text-fg-muted">
                  Concurrency limit: {concurrency}
                </label>
                <Slider
                  id="ov-conc"
                  value={[concurrency]}
                  onValueChange={([v]) => setConcurrency(v)}
                  max={100}
                  aria-label="Concurrency limit"
                />
              </div>

              <div className="flex items-center gap-3 [grid-column:1/-1]">
                <Button type="submit" loading={saving} disabled={emailInvalid}>
                  {saving ? "Saving…" : "Save settings"}
                </Button>
                <Text size="sm" tone="muted">
                  Submits with a real loading state, then fires a toast.
                </Text>
              </div>
            </form>
          </Section>

          <Section id="ov-overlays" title="Overlays">
            <Row>
              <Dialog>
                <DialogTrigger asChild>
                  <Button variant="danger">Delete project…</Button>
                </DialogTrigger>
                <DialogContent>
                  <DialogHeader>
                    <DialogTitle>Delete this project?</DialogTitle>
                    <DialogDescription>
                      This removes the project, its deployments, and its environment variables. This
                      cannot be undone.
                    </DialogDescription>
                  </DialogHeader>
                  <DialogFooter>
                    <DialogClose asChild>
                      <Button variant="secondary">Cancel</Button>
                    </DialogClose>
                    <DialogClose asChild>
                      <Button variant="danger">Delete</Button>
                    </DialogClose>
                  </DialogFooter>
                </DialogContent>
              </Dialog>

              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="secondary">Row actions</Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent>
                  <DropdownMenuItem>Redeploy</DropdownMenuItem>
                  <DropdownMenuItem>Copy URL</DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="danger">Delete</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>

              <Popover>
                <PopoverTrigger asChild>
                  <Button variant="secondary">Filter</Button>
                </PopoverTrigger>
                <PopoverContent aria-label="Filter deployments">
                  <div className="flex flex-col gap-3">
                    <label className="flex items-center gap-2">
                      <Checkbox defaultChecked /> <Text size="sm">Production</Text>
                    </label>
                    <label className="flex items-center gap-2">
                      <Checkbox defaultChecked /> <Text size="sm">Preview</Text>
                    </label>
                  </div>
                </PopoverContent>
              </Popover>

              <Popover>
                <PopoverTrigger asChild>
                  <Button variant="ghost" aria-label="Help">
                    ?
                  </Button>
                </PopoverTrigger>
                <PopoverContent aria-label="Help">
                  Deploys trigger on every push to a connected branch.
                </PopoverContent>
              </Popover>

              <Button
                variant="secondary"
                onClick={() =>
                  toast({ title: "Redeploy queued", description: "main → Production" })
                }
              >
                Fire a toast
              </Button>
            </Row>
          </Section>

          <Section id="ov-feedback" title="Feedback &amp; status">
            <div className="flex flex-col gap-3">
              <Alert variant="accent" title="Heads up">
                A new region is available for deployments.
              </Alert>
              <Alert variant="success" title="Deployed">
                forsight-client-portal is live in production.
              </Alert>
              <Alert variant="danger" title="Build failed">
                Type error in <code className="font-mono">src/routes/api.ts</code>.
              </Alert>
              <Row>
                <div className="min-w-0 flex-1">
                  <Progress value={68} aria-label="Upload progress" />
                </div>
                <Spinner label="Loading" />
              </Row>
              <Row>
                <Skeleton className="h-10 w-10 rounded-full" />
                <Skeleton className="h-3 w-40" />
                <Skeleton className="h-3 w-24" />
              </Row>
            </div>
          </Section>

          <Section id="ov-people" title="People">
            <Row>
              <Avatar initials="MF" alt="Marc Fors" />
              <Avatar initials="JD" alt="Jamie Doe" size="lg" />
              <AvatarGroup max={3}>
                <Avatar initials="MF" alt="Marc Fors" />
                <Avatar initials="JD" alt="Jamie Doe" />
                <Avatar initials="AK" alt="Alex Kim" />
                <Avatar initials="RL" alt="Robin Lee" />
                <Avatar initials="SP" alt="Sam Park" />
              </AvatarGroup>
            </Row>
          </Section>

          <Section id="ov-data" title="Data &amp; layout">
            <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(16rem,1fr))]">
              <Card>
                <CardHeader>
                  <CardTitle>Rapids plan</CardTitle>
                  <CardDescription>For teams shipping client work every week.</CardDescription>
                </CardHeader>
                <CardContent>Unlimited projects, custom branding, priority support.</CardContent>
                <CardFooter>
                  <Button size="sm">Choose plan</Button>
                  <Button size="sm" variant="ghost">
                    Compare
                  </Button>
                </CardFooter>
              </Card>
              <Card>
                <Tabs.Root defaultValue="overview">
                  <Tabs.List>
                    <Tabs.Trigger value="overview">Overview</Tabs.Trigger>
                    <Tabs.Trigger value="activity">Activity</Tabs.Trigger>
                    <Tabs.Trigger value="settings">Settings</Tabs.Trigger>
                  </Tabs.List>
                  <Tabs.Panel value="overview" className="pt-3">
                    Deployment health, traffic, and error rate at a glance.
                  </Tabs.Panel>
                  <Tabs.Panel value="activity" className="pt-3">
                    Recent deploys and rollbacks.
                  </Tabs.Panel>
                  <Tabs.Panel value="settings" className="pt-3">
                    Environment variables and access.
                  </Tabs.Panel>
                </Tabs.Root>
              </Card>
            </div>

            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Branch</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Environment</TableHead>
                  <TableHead>Deployed</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {deployments.map((d) => (
                  <TableRow key={d.id}>
                    <TableCell className="font-mono">{d.branch}</TableCell>
                    <TableCell>
                      <Badge variant={statusVariant[d.status]}>{d.status}</Badge>
                    </TableCell>
                    <TableCell>{d.env}</TableCell>
                    <TableCell>{d.when}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>

            <Accordion
              type="single"
              collapsible
              defaultValue="a"
              className="rounded-lg border border-ink-border px-4"
            >
              <AccordionItem value="a">
                <AccordionTrigger>How does billing work?</AccordionTrigger>
                <AccordionContent>
                  Billed monthly by plan. Upgrades apply immediately; downgrades next cycle.
                </AccordionContent>
              </AccordionItem>
              <AccordionItem value="b">
                <AccordionTrigger>Can I cancel anytime?</AccordionTrigger>
                <AccordionContent>Yes — you keep access until the period ends.</AccordionContent>
              </AccordionItem>
            </Accordion>
          </Section>

          <Section id="ov-nav" title="Navigation">
            <Breadcrumb>
              <BreadcrumbList>
                {["Projects", "forsight-client-portal"].map((crumb) => (
                  <React.Fragment key={crumb}>
                    <BreadcrumbItem>
                      <BreadcrumbLink
                        href="#ov-nav"
                        onClick={(e) => {
                          e.preventDefault();
                          toast({
                            title: `Navigate to ${crumb}`,
                            description: "(demo — no routing here)",
                          });
                        }}
                      >
                        {crumb}
                      </BreadcrumbLink>
                    </BreadcrumbItem>
                    <BreadcrumbSeparator />
                  </React.Fragment>
                ))}
                <BreadcrumbItem>
                  <BreadcrumbPage>Settings</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>

            <div className="flex flex-col gap-2">
              <Pagination>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={page === 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                >
                  Prev
                </Button>
                {pageWindow.map((n, i) => (
                  <React.Fragment key={n}>
                    {i > 0 && n - pageWindow[i - 1] > 1 && <PaginationEllipsis />}
                    <PaginationItem active={n === page} onClick={() => setPage(n)}>
                      {n}
                    </PaginationItem>
                  </React.Fragment>
                ))}
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={page === TOTAL_PAGES}
                  onClick={() => setPage((p) => Math.min(TOTAL_PAGES, p + 1))}
                >
                  Next
                </Button>
              </Pagination>
              <Text size="sm" tone="muted" aria-live="polite">
                Page {page} of {TOTAL_PAGES}
              </Text>
            </div>
          </Section>

          <Section id="ov-shell" title="App shell">
            {/* A contained preview, not a real full-page AppShellMain — this
                page already has its own <main> landmark, and nesting a
                second one would be an actual accessibility violation. See
                Sidebar.stories.tsx's own AppShellExample for the real,
                standalone full-page composition. */}
            <div className="h-80 overflow-hidden rounded-lg border border-ink-border">
              <SidebarProvider>
                <AppShell className="h-full">
                  <Sidebar label="Demo navigation">
                    <SidebarHeader>
                      <Text weight="semibold">Forsight</Text>
                    </SidebarHeader>
                    <SidebarContent>
                      <SidebarNav>
                        <SidebarNavItem href="#ov-shell" active>
                          Overview
                        </SidebarNavItem>
                        <SidebarNavItem href="#ov-shell">Settings</SidebarNavItem>
                      </SidebarNav>
                    </SidebarContent>
                    <SidebarFooter>
                      <Text size="sm" tone="muted">
                        v1.0
                      </Text>
                    </SidebarFooter>
                  </Sidebar>
                  <div className="min-w-0 flex-1 overflow-y-auto p-4">
                    <SidebarTrigger />
                    <Text className="mt-3" tone="secondary">
                      Collapses to an icon rail on desktop, becomes a slide-in drawer below the md
                      breakpoint.
                    </Text>
                  </div>
                </AppShell>
              </SidebarProvider>
            </div>
          </Section>

          <footer className="border-t border-ink-border pt-4">
            <Text size="sm" tone="muted">
              {Object.keys(LIGHT_PALETTE).length} tokens per theme · switch the Theme toolbar above
              to see light mode · resize the window to check responsiveness.
            </Text>
          </footer>
        </div>
        <Toaster />
        <CommandDialog
          open={commandOpen}
          onOpenChange={setCommandOpen}
          label="Quick actions"
          description="Search for an action to run against forsight-client-portal."
        >
          <CommandInput placeholder="Type a command…" />
          <CommandList>
            <CommandEmpty>No results found.</CommandEmpty>
            <CommandGroup heading="Deploy">
              <CommandItem
                onSelect={() =>
                  runCommand(() =>
                    toast({ title: "Redeploy queued", description: "main → Production" })
                  )
                }
              >
                Redeploy production
              </CommandItem>
              <CommandItem onSelect={() => runCommand(() => setPage(1))}>
                Jump to first deployment page
              </CommandItem>
            </CommandGroup>
            <CommandGroup heading="Settings">
              <CommandItem onSelect={() => runCommand(() => setNotify((v) => !v))}>
                Toggle failure emails
              </CommandItem>
              <CommandItem onSelect={() => runCommand(() => setPreviews((v) => !v))}>
                Toggle preview deployments
              </CommandItem>
            </CommandGroup>
          </CommandList>
        </CommandDialog>
      </main>
    </TooltipProvider>
  );
}

export const Kitchen: Story = {
  render: () => <KitchenSink />,
};

/**
 * The same page under `dir="rtl"` — the fastest whole-system check that
 * logical-property/`rtl:` conversions actually hold together, the same
 * role `Kitchen` plays for the light/dark themes. Verify at 375px and
 * desktop, same as `Kitchen`.
 */
export const KitchenSinkRTL: Story = {
  render: () => (
    <div dir="rtl">
      <KitchenSink />
    </div>
  ),
};

/* ── Observability dashboard ───────────────────────────────────────────────
 * The data-viz and observability families composed the way a real service
 * dashboard uses them. This is the whole-system check for those components,
 * the way `Kitchen` is for the base library: render it at 375px and desktop,
 * in both themes and both directions.
 */

const HOURS = Array.from({ length: 24 }, (_, i) => `${String(i).padStart(2, "0")}:00`);
const RPS = [
  820, 760, 690, 640, 610, 640, 780, 1120, 1580, 1720, 1690, 1740, 1810, 1770, 1690, 1620, 1580,
  1490, 1380, 1240, 1120, 1010, 940, 870,
];
const P95 = [
  180, 172, 168, 165, 162, 170, 195, 240, 320, 356, 344, 351, 388, 372, 340, 318, 305, 288, 262,
  240, 224, 210, 198, 190,
];
const UPTIME = Array.from({ length: 60 }, (_, i) => ({
  label: `Day ${i + 1}`,
  status:
    i === 41 ? ("outage" as const) : i === 42 ? ("degraded" as const) : ("operational" as const),
  detail: i === 41 ? "primary database failover" : undefined,
}));

function ObservabilityDashboard() {
  const [range, setRange] = React.useState("24h");

  return (
    <main className="min-h-screen bg-ink-bg p-4 sm:p-8 lg:p-10">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <Heading as="h1" size="lg">
              checkout-api
            </Heading>
            <div className="mt-1 flex items-center gap-3">
              <StatusDot status="operational" />
              <Text size="sm" tone="muted">
                us-east · production
              </Text>
            </div>
          </div>
          <TimeRange
            label="Dashboard time range"
            value={range}
            onValueChange={setRange}
            options={[
              { value: "1h", label: "1h", description: "Last 1 hour" },
              { value: "6h", label: "6h", description: "Last 6 hours" },
              { value: "24h", label: "24h", description: "Last 24 hours" },
              { value: "7d", label: "7d", description: "Last 7 days" },
            ]}
          />
        </header>

        <Section id="dash-signals" title="Key signals">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard
              label="Requests"
              value="1.24M"
              delta={8.4}
              deltaCaption="vs. previous 24h"
              trend={RPS}
            />
            <StatCard
              label="p95 latency"
              value="248"
              unit="ms"
              delta={-12.4}
              deltaGoodDirection="down"
              deltaCaption="vs. previous 24h"
              trend={P95}
            />
            <StatCard
              label="Error rate"
              value="0.42"
              unit="%"
              delta={0.1}
              deltaGoodDirection="down"
              deltaCaption="vs. previous 24h"
              status="operational"
            />
            <StatCard label="Deploys" value="14" delta={0} deltaCaption="vs. previous 24h" />
          </div>
        </Section>

        <Section id="dash-traffic" title="Traffic and latency">
          <div className="grid gap-4 lg:grid-cols-3">
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>Throughput and latency</CardTitle>
                <CardDescription>Two measures, two plots — never two y-axes.</CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-6">
                <LineChart
                  label="Requests per second"
                  description="checkout-api, last 24 hours"
                  labels={HOURS}
                  series={[{ name: "checkout-api", values: RPS }]}
                  area
                  height={180}
                />
                <LineChart
                  label="p95 latency by region"
                  description="Milliseconds, last 24 hours"
                  labels={HOURS}
                  series={[
                    { name: "us-east", values: P95 },
                    { name: "eu-west", values: P95.map((v) => Math.round(v * 0.82)) },
                    { name: "ap-south", values: P95.map((v) => Math.round(v * 1.31)) },
                  ]}
                  valueFormat={(value) => `${value}ms`}
                  height={180}
                />
              </CardContent>
            </Card>

            <div className="flex flex-col gap-4">
              <Card className="flex items-center justify-center">
                <Gauge
                  label="Error budget"
                  caption="of the 30-day budget remaining"
                  value={62}
                  target={25}
                  valueFormat={(value) => formatPercent(value, 0)}
                />
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle>Traffic by region</CardTitle>
                </CardHeader>
                <CardContent>
                  <DonutChart
                    label="Traffic by region"
                    size={160}
                    centerLabel="requests"
                    data={[
                      { name: "us-east", value: 482_000 },
                      { name: "eu-west", value: 291_000 },
                      { name: "ap-south", value: 118_000 },
                      { name: "sa-east", value: 39_000 },
                    ]}
                  />
                </CardContent>
              </Card>
            </div>
          </div>
        </Section>

        <Section id="dash-breakdown" title="Breakdowns">
          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Responses by status class</CardTitle>
              </CardHeader>
              <CardContent>
                <BarChart
                  label="Responses by status class"
                  labels={["10:00", "11:00", "12:00", "13:00", "14:00", "15:00"]}
                  series={[
                    { name: "2xx", values: [1200, 1420, 1310, 1680, 1520, 1390] },
                    { name: "4xx", values: [90, 120, 105, 260, 180, 140] },
                    { name: "5xx", values: [4, 6, 3, 88, 22, 9] },
                  ]}
                  stacked
                  height={200}
                />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Slowest routes</CardTitle>
                <CardDescription>p95, last hour</CardDescription>
              </CardHeader>
              <CardContent>
                <BarList
                  valueFormat={formatDuration}
                  items={[
                    { label: "POST /api/checkout", value: 1840 },
                    { label: "GET /api/search", value: 920 },
                    { label: "POST /api/cart", value: 410 },
                    { label: "GET /api/products", value: 264 },
                  ]}
                />
              </CardContent>
            </Card>
          </div>
        </Section>

        <Section id="dash-reliability" title="Reliability">
          <Card>
            <CardHeader>
              <CardTitle>Availability</CardTitle>
            </CardHeader>
            <CardContent>
              <UptimeBar
                label="checkout-api"
                segments={UPTIME}
                startCaption="60 days ago"
                endCaption="Today"
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>5xx by service and hour</CardTitle>
            </CardHeader>
            <CardContent>
              <Heatmap
                label="5xx responses by service and hour (UTC)"
                columns={Array.from({ length: 12 }, (_, i) => String(i * 2).padStart(2, "0"))}
                rows={[
                  { label: "checkout", values: [0, 0, 2, 1, 0, 4, 18, 44, 96, 31, 6, 1] },
                  { label: "search", values: [1, 0, 0, 0, 2, 3, 5, 9, 12, 7, 2, 0] },
                  { label: "payments", values: [0, 0, 0, 0, 0, 0, 1, 3, 22, 4, 0, 0] },
                ]}
              />
            </CardContent>
          </Card>
        </Section>

        <Section id="dash-diagnostics" title="Diagnostics">
          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Incident 4f21a</CardTitle>
              </CardHeader>
              <CardContent>
                <Timeline
                  items={[
                    {
                      id: "1",
                      time: "14:02",
                      title: "Alert fired — checkout 5xx above 2%",
                      tone: "danger",
                    },
                    { id: "2", time: "14:05", title: "Acknowledged by @marc", tone: "warning" },
                    { id: "3", time: "14:18", title: "Rolled back to 3e90c", tone: "accent" },
                    { id: "4", time: "14:31", title: "Resolved", tone: "success" },
                  ]}
                />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Recent logs</CardTitle>
              </CardHeader>
              <CardContent>
                <LogStream
                  label="checkout-api production logs"
                  maxHeight={220}
                  entries={[
                    {
                      id: "1",
                      timestamp: "14:02:03",
                      level: "info",
                      message: "starting rollout 4f21a",
                      source: "deployer",
                    },
                    {
                      id: "2",
                      timestamp: "14:02:19",
                      level: "warn",
                      message: "readiness probe failed: connection refused",
                      source: "checkout-7f9",
                    },
                    {
                      id: "3",
                      timestamp: "14:02:22",
                      level: "error",
                      message: "dial tcp 10.4.1.22:5432: i/o timeout",
                      source: "checkout-7f9",
                    },
                    {
                      id: "4",
                      timestamp: "14:18:44",
                      level: "info",
                      message: "rolled back to 3e90c",
                      source: "deployer",
                    },
                  ]}
                />
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Slowest trace in the window</CardTitle>
            </CardHeader>
            <CardContent>
              <TraceWaterfall
                label="POST /api/checkout — trace 9f2c41"
                spans={[
                  {
                    id: "1",
                    name: "POST /api/checkout",
                    service: "edge",
                    start: 0,
                    duration: 1840,
                  },
                  {
                    id: "2",
                    name: "auth.verify",
                    service: "identity",
                    start: 12,
                    duration: 96,
                    depth: 1,
                  },
                  {
                    id: "3",
                    name: "cart.load",
                    service: "checkout",
                    start: 120,
                    duration: 210,
                    depth: 1,
                  },
                  {
                    id: "4",
                    name: "pricing.quote",
                    service: "pricing",
                    start: 340,
                    duration: 420,
                    depth: 1,
                  },
                  {
                    id: "5",
                    name: "payment.charge",
                    service: "payments",
                    start: 780,
                    duration: 980,
                    depth: 1,
                    status: "error",
                  },
                ]}
              />
            </CardContent>
          </Card>
        </Section>
      </div>
    </main>
  );
}

export const Dashboard: Story = {
  render: () => <ObservabilityDashboard />,
};

/**
 * The same dashboard under `dir="rtl"` — the whole-system RTL check for the
 * chart/observability family, the way `KitchenSinkRTL` is for the base
 * library (roadmap "Fold seven chart components into the DashboardRTL
 * story"): every chart `Dashboard` composes — `LineChart` (x2), `BarChart`,
 * `DonutChart`, `BarList`, `Heatmap`, `TraceWaterfall`, and `Sparkline` (via
 * `StatCard`'s `trend`) — renders here too, since this story wraps the exact
 * same `ObservabilityDashboard`, so the Storybook test runner's axe pass
 * already covers all seven under `dir="rtl"`.
 *
 * The `play` step below goes further than axe and checks the geometry axe
 * can't: a chart's plotted x-axis is a physical pixel space that never
 * mirrors under RTL (see `ChartFrame.tsx`'s "time flows left-to-right in
 * both text directions"), so a handful of spots that used a *logical*
 * left/right property against that physical space rendered wrong under
 * `dir="rtl"` — axis-value labels landing on top of the plot instead of in
 * its margin (SVG's `text-anchor` resolves against the inherited CSS
 * direction), the keyboard-cursor tooltip parking on the same side as the
 * point instead of the opposite one, and `TraceWaterfall`'s span bars
 * reading the trace backwards. Fixed in `ChartFrame.tsx`, `LineChart.tsx`,
 * `BarChart.tsx` and `TraceWaterfall.tsx`; verified here by real rendered
 * position (`getBoundingClientRect`), not `toHaveClass` — see
 * `Switch.stories.tsx`'s "RTL" story for why.
 */
export const DashboardRTL: Story = {
  render: () => (
    <div dir="rtl">
      <ObservabilityDashboard />
    </div>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);

    // --- LineChart: a y-axis value label sits in the left margin, not on
    // top of the plotted line, and the keyboard-cursor tooltip parks on
    // the side that actually avoids the active point.
    const requestsSvg = [...canvasElement.querySelectorAll('svg[role="img"]')].find(
      (svg) => svg.querySelector("title")?.textContent === "Requests per second"
    );
    if (!requestsSvg) throw new Error("Requests per second chart not found");
    const plot = requestsSvg.closest<HTMLElement>('[tabindex="0"]');
    // dominant-baseline="middle" is only set on the y-axis tick labels
    // (LineChart.tsx), not the x-axis category labels — both use
    // text-anchor="end" for their last entry.
    const tick = [...requestsSvg.querySelectorAll("text")].find(
      (t) =>
        t.getAttribute("text-anchor") === "end" && t.getAttribute("dominant-baseline") === "middle"
    );
    const plotLeft = Math.min(
      ...[...requestsSvg.querySelectorAll("path")].map((p) => p.getBoundingClientRect().left)
    );
    if (!plot || !tick) throw new Error("LineChart geometry not found");
    await expect(tick.getBoundingClientRect().right).toBeLessThanOrEqual(plotLeft + 2);

    plot.focus();
    await userEvent.keyboard("{End}");
    const circle = requestsSvg.querySelector("circle");
    const tooltip = plot.querySelector<HTMLElement>(":scope > div.absolute");
    if (!circle || !tooltip) throw new Error("Keyboard-cursor readout not found");
    await expect(tooltip.getBoundingClientRect().right).toBeLessThanOrEqual(
      circle.getBoundingClientRect().left + 2
    );
    await userEvent.keyboard("{Escape}");

    // --- TraceWaterfall: a span's bar is positioned by wall-clock offset,
    // which (like the line/bar x-axis above) reads left-to-right in both
    // text directions — a span that starts later renders further right,
    // never further left, regardless of page direction. The root span is
    // deliberately not one of the two compared: at 100% width its bar fills
    // the row either way, so it can't tell a fixed timeline from a mirrored
    // one — `auth.verify` (starts at 12ms) and `payment.charge` (starts at
    // 780ms) both start and end short of the full width, so their bars only
    // land in the right order when the offset reads left-to-right.
    const earlyRow = canvas.getByRole("rowheader", { name: /auth\.verify/ }).closest("tr");
    const laterRow = canvas.getByRole("rowheader", { name: /payment\.charge/ }).closest("tr");
    const earlyBar = earlyRow?.querySelector<HTMLElement>("td span span");
    const laterBar = laterRow?.querySelector<HTMLElement>("td span span");
    if (!earlyBar || !laterBar) throw new Error("TraceWaterfall bars not found");
    await expect(earlyBar.getBoundingClientRect().left).toBeLessThan(
      laterBar.getBoundingClientRect().left
    );
  },
};
