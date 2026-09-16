import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import * as React from "react";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
  Alert,
  AlertList,
  Avatar,
  AvatarGroup,
  Badge,
  BarChart,
  BarList,
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
  BoxPlot,
  Button,
  Calendar,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  ChartLegend,
  Checkbox,
  ComboChart,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  CodeBlock,
  CopyButton,
  Delta,
  DonutChart,
  EmptyState,
  ErrorBudget,
  Funnel,
  Gauge,
  Heading,
  Heatmap,
  Histogram,
  Input,
  JSONViewer,
  Kbd,
  Label,
  LineChart,
  LogStream,
  Pagination,
  PaginationEllipsis,
  PaginationItem,
  Progress,
  RadioGroup,
  RadioGroupItem,
  ScrollArea,
  Separator,
  Sidebar,
  SidebarHeader,
  SidebarNav,
  SidebarNavItem,
  SidebarProvider,
  Skeleton,
  Slider,
  Sparkline,
  Spinner,
  Stepper,
  StatCard,
  StatusDot,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  Text,
  Textarea,
  Toggle,
  ToggleGroup,
  ToggleGroupItem,
  TimeRange,
  Timeline,
  TraceWaterfall,
  UptimeBar,
} from "../index";

/**
 * Structural-regression tripwire. Each case renders one component in a
 * canonical state and snapshots its serialized DOM. A refactor that
 * unintentionally drops a class (`focus-visible:shadow-focus-ring`, a
 * token color), changes an element or an ARIA attribute, or reorders the
 * markup shows up here as a diff to review.
 *
 * When a change IS intentional, re-generate with `npx vitest run -u`
 * and eyeball the snapshot diff in the PR.
 *
 * Portal-only overlays (Dialog / AlertDialog / Drawer / DropdownMenu /
 * Popover / Tooltip / HoverCard / Select / Combobox / MultiSelect /
 * DatePicker / DateRangePicker / FilterBar's add-filter menu / Toast
 * content) are exercised open, by keyboard, in their own `*.test.tsx` and
 * in the Storybook test runner — snapshotting a closed trigger here would
 * add churn without coverage. `Calendar` itself is plain in-flow markup
 * (not a portal), so it's snapshotted directly below, including its
 * `range` mode.
 */
const cases: Record<string, React.ReactElement> = {
  "Button/primary": <Button>Deploy</Button>,
  "Button/all-variants": (
    <div>
      {(["primary", "secondary", "spark", "ghost", "danger"] as const).map((v) => (
        <Button key={v} variant={v}>
          {v}
        </Button>
      ))}
    </div>
  ),
  "Button/loading": <Button loading>Deploying</Button>,
  "Badge/all-variants": (
    <div>
      {(["neutral", "accent", "spark", "success", "warning", "danger"] as const).map((v) => (
        <Badge key={v} variant={v}>
          {v}
        </Badge>
      ))}
    </div>
  ),
  "Input/with-hint-invalid": (
    <Input hint="That email is taken." invalid defaultValue="x" readOnly />
  ),
  "Textarea/with-hint": <Textarea hint="Markdown supported." readOnly />,
  "Checkbox/checked": <Checkbox defaultChecked aria-label="Agree" />,
  "Switch/on": <Switch defaultChecked aria-label="Previews" />,
  "RadioGroup/two-options": (
    <RadioGroup defaultValue="a" aria-label="Plan">
      <RadioGroupItem value="a" aria-label="A" />
      <RadioGroupItem value="b" aria-label="B" />
    </RadioGroup>
  ),
  "Slider/single": <Slider defaultValue={[40]} max={100} aria-label="Limit" />,
  "Toggle/pressed": (
    <Toggle aria-label="Pin sidebar" defaultPressed>
      Pin
    </Toggle>
  ),
  "ToggleGroup/single-select": (
    <ToggleGroup type="single" defaultValue="chart" aria-label="View mode">
      <ToggleGroupItem value="table">Table</ToggleGroupItem>
      <ToggleGroupItem value="chart">Chart</ToggleGroupItem>
    </ToggleGroup>
  ),
  "ToggleGroup/multi-select": (
    <ToggleGroup type="multiple" defaultValue={["errors"]} aria-label="Log levels shown">
      <ToggleGroupItem value="info">Info</ToggleGroupItem>
      <ToggleGroupItem value="errors">Errors</ToggleGroupItem>
    </ToggleGroup>
  ),
  "Alert/danger": (
    <Alert variant="danger" title="Build failed">
      Type error in api.ts
    </Alert>
  ),
  "Progress/68": <Progress value={68} aria-label="Upload" />,
  "Spinner/md": <Spinner label="Loading" />,
  "Skeleton/line": <Skeleton className="h-4 w-32" />,
  "Stepper/middle-step": (
    <Stepper
      label="Setup progress"
      steps={[{ label: "Account" }, { label: "Team" }, { label: "Billing" }]}
      currentStep={1}
    />
  ),
  "Avatar/initials": <Avatar initials="MF" alt="Marc Fors" />,
  "AvatarGroup/overflow": (
    <AvatarGroup max={2}>
      <Avatar initials="MF" alt="Marc Fors" />
      <Avatar initials="JD" alt="Jamie Doe" />
      <Avatar initials="AK" alt="Alex Kim" />
    </AvatarGroup>
  ),
  "Heading/h2": (
    <Heading as="h2" size="lg">
      Deployments
    </Heading>
  ),
  "Text/secondary": <Text tone="secondary">Supporting copy</Text>,
  "Card/full": (
    <Card>
      <CardHeader>
        <CardTitle>Plan</CardTitle>
      </CardHeader>
      <CardContent>Body</CardContent>
      <CardFooter>
        <Button size="sm">Go</Button>
      </CardFooter>
    </Card>
  ),
  "EmptyState/with-description": (
    <EmptyState title="No deployments yet" description="Push to see them here." />
  ),
  "Tabs/default": (
    <Tabs.Root defaultValue="a">
      <Tabs.List>
        <Tabs.Trigger value="a">A</Tabs.Trigger>
        <Tabs.Trigger value="b">B</Tabs.Trigger>
      </Tabs.List>
      <Tabs.Panel value="a">Panel A</Tabs.Panel>
    </Tabs.Root>
  ),
  "Accordion/one-open": (
    <Accordion type="single" collapsible defaultValue="a">
      <AccordionItem value="a">
        <AccordionTrigger>Q</AccordionTrigger>
        <AccordionContent>A</AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
  "Breadcrumb/trail": (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="/">Home</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator />
        <BreadcrumbItem>
          <BreadcrumbPage>Now</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  ),
  "Pagination/strip": (
    <Pagination>
      <PaginationItem active>1</PaginationItem>
      <PaginationItem>2</PaginationItem>
      <PaginationEllipsis />
      <PaginationItem>9</PaginationItem>
    </Pagination>
  ),
  "Table/rows": (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Branch</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow>
          <TableCell>main</TableCell>
        </TableRow>
      </TableBody>
    </Table>
  ),
  "Table/sortable-header": (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead sortDirection="ascending" onSort={() => {}}>
            Deployed
          </TableHead>
        </TableRow>
      </TableHeader>
    </Table>
  ),
  "Calendar/range-selection": (
    <Calendar
      mode="range"
      selected={{ from: new Date(2026, 8, 8), to: new Date(2026, 8, 11) }}
      defaultMonth={new Date(2026, 8, 1)}
      // Pinned: react-day-picker puts a `today` modifier on the current date,
      // so without this the rendered markup — and therefore this snapshot —
      // changes at every midnight. The pinned day sits inside the selected
      // range on purpose, so the today + range_middle combination stays
      // covered rather than being sidestepped.
      today={new Date(2026, 8, 9)}
    />
  ),
  "Separator/horizontal": <Separator />,
  "Separator/vertical": <Separator orientation="vertical" />,
  "Label/basic": <Label htmlFor="x">Workspace name</Label>,
  "JSONViewer/nested-object": (
    <JSONViewer label="Span attributes" data={{ service: "checkout-api", http: { status: 500 } }} />
  ),
  "Kbd/shortcut": (
    <span>
      <Kbd>⌘</Kbd>
      <Kbd>K</Kbd>
    </span>
  ),
  "CopyButton/idle": <CopyButton value="hello world" />,
  "CodeBlock/with-label": <CodeBlock code="npm install forsight" label="Terminal" />,
  "Collapsible/closed": (
    <Collapsible>
      <CollapsibleTrigger>Show more</CollapsibleTrigger>
      <CollapsibleContent>Extra detail</CollapsibleContent>
    </Collapsible>
  ),
  // The custom scrollbar/thumb only mount once Radix measures real overflow
  // (a real layout engine, which jsdom doesn't have) — this snapshot covers
  // the static wrapper markup only; see ScrollArea.test.tsx.
  "ScrollArea/static": (
    <ScrollArea className="h-40 w-64">
      <p>Plenty of content that would overflow in a real browser.</p>
    </ScrollArea>
  ),
  "Sidebar/expanded": (
    <SidebarProvider>
      <Sidebar label="Main navigation">
        <SidebarHeader>Forsight</SidebarHeader>
        <SidebarNav>
          <SidebarNavItem href="#a" active>
            Overview
          </SidebarNavItem>
        </SidebarNav>
      </Sidebar>
    </SidebarProvider>
  ),
  "Sidebar/collapsed": (
    <SidebarProvider defaultCollapsed>
      <Sidebar label="Main navigation">
        <SidebarNav>
          <SidebarNavItem href="#a">Overview</SidebarNavItem>
        </SidebarNav>
      </Sidebar>
    </SidebarProvider>
  ),
  // ── Data visualization ────────────────────────────────────────────────────
  // Charts measure their own width; under jsdom (no ResizeObserver callback)
  // they render at the fallback width, so the geometry here is deterministic.
  "LineChart/two-series-with-gap": (
    <LineChart
      label="Requests per second"
      labels={["12:00", "13:00", "14:00"]}
      series={[
        { name: "us-east", values: [120, 180, 140] },
        { name: "eu-west", values: [90, null, 130] },
      ]}
    />
  ),
  "BarChart/stacked": (
    <BarChart
      label="Responses by status class"
      labels={["2xx", "4xx", "5xx"]}
      series={[
        { name: "checkout", values: [1200, 90, 12] },
        { name: "search", values: [800, 60, 4] },
      ]}
      stacked
    />
  ),
  "Sparkline/line": <Sparkline label="Error rate" values={[4, 9, 6, 12, 8]} />,
  "BarList/ranked": (
    <BarList
      items={[
        { label: "/api/checkout", value: 1240 },
        { label: "/api/search", value: 620 },
      ]}
    />
  ),
  "Funnel/narrowing-stages": (
    <Funnel
      stages={[
        { label: "Visited pricing page", value: 1000 },
        { label: "Started signup", value: 400 },
        { label: "Became a paying customer", value: 100 },
      ]}
    />
  ),
  "DonutChart/three-slices": (
    <DonutChart
      label="Traffic by region"
      data={[
        { name: "us-east", value: 60 },
        { name: "eu-west", value: 30 },
        { name: "ap-south", value: 10 },
      ]}
    />
  ),
  "Heatmap/two-rows": (
    <Heatmap
      label="Errors by service and hour"
      columns={["00", "01", "02"]}
      rows={[
        { label: "checkout", values: [0, 12, 40] },
        { label: "search", values: [4, null, 8] },
      ]}
    />
  ),
  "ComboChart/bar-and-line": (
    <ComboChart
      label="Requests and latency"
      labels={["12:00", "13:00", "14:00"]}
      series={[
        { name: "Requests", type: "bar", values: [1200, 1800, 1400] },
        { name: "p99 latency", type: "line", values: [180, 340, 190] },
      ]}
    />
  ),
  "BoxPlot/two-boxes": (
    <BoxPlot
      label="Latency spread"
      boxes={[
        { label: "checkout", min: 40, q1: 80, median: 120, q3: 180, max: 420 },
        { label: "search", min: 20, q1: 35, median: 50, q3: 70, max: 160 },
      ]}
    />
  ),
  "Histogram/buckets": (
    <Histogram
      label="Request duration"
      buckets={[
        { label: "0–50ms", count: 420 },
        { label: "50–100ms", count: 980 },
      ]}
    />
  ),
  "Gauge/with-target": (
    <Gauge label="Error budget" value={62} target={80} caption="of 30-day budget" />
  ),
  "ChartLegend/two-series": (
    <ChartLegend
      items={[
        { label: "us-east", seriesIndex: 0, value: "62%" },
        { label: "eu-west", seriesIndex: 1, value: "38%" },
      ]}
    />
  ),

  // ── Observability ─────────────────────────────────────────────────────────
  "StatCard/full": (
    <StatCard
      label="p95 latency"
      value="248"
      unit="ms"
      delta={-12}
      deltaGoodDirection="down"
      deltaCaption="vs. previous 24h"
      trend={[310, 290, 265, 248]}
      status="operational"
    />
  ),
  "Delta/increase-is-bad": <Delta value={30.2} goodDirection="down" />,
  "ErrorBudget/at-risk": (
    <ErrorBudget label="30-day error budget" consumed={78} caption="Resets in 3 days" />
  ),
  "AlertList/active-and-resolved": (
    <AlertList
      label="Active alerts"
      items={[
        { id: "1", severity: "critical", title: "Elevated 5xx rate", time: "2 min ago" },
        {
          id: "2",
          severity: "info",
          title: "Disk usage above 80%",
          time: "20 min ago",
          resolved: true,
        },
      ]}
    />
  ),
  "StatusDot/degraded": <StatusDot status="degraded" pulse />,
  "UptimeBar/with-incident": (
    <UptimeBar
      label="checkout-api"
      segments={[
        { label: "1 Mar", status: "operational" },
        { label: "2 Mar", status: "degraded", detail: "elevated p99" },
        { label: "3 Mar", status: "operational" },
      ]}
      startCaption="3 days ago"
      endCaption="Today"
    />
  ),
  "LogStream/two-lines": (
    <LogStream
      label="checkout-api logs"
      entries={[
        { id: "1", timestamp: "14:02:11", level: "info", message: "deploy started", source: "ci" },
        { id: "2", timestamp: "14:02:44", level: "error", message: "connection refused" },
      ]}
    />
  ),
  "Timeline/incident": (
    <Timeline
      items={[
        { id: "1", time: "14:02", title: "Alert fired", tone: "danger" },
        { id: "2", time: "14:31", title: "Resolved", tone: "success" },
      ]}
    />
  ),
  "TraceWaterfall/nested-spans": (
    <TraceWaterfall
      label="POST /checkout"
      spans={[
        { id: "a", name: "POST /checkout", service: "edge", start: 0, duration: 400 },
        {
          id: "b",
          name: "charge",
          service: "payments",
          start: 200,
          duration: 180,
          depth: 1,
          status: "error",
        },
      ]}
    />
  ),
  "TimeRange/segmented": (
    <TimeRange
      label="Dashboard time range"
      options={[
        { value: "1h", label: "1h", description: "Last 1 hour" },
        { value: "24h", label: "24h", description: "Last 24 hours" },
      ]}
      value="24h"
      onValueChange={() => {}}
    />
  ),
  // Appended, not interleaved with the other LineChart case above: every
  // case before it renders `useId()`-based ids, so inserting a new one
  // earlier in this record shifts every id after it and fails their
  // snapshots for a reason that has nothing to do with the change.
  "LineChart/projection": (
    <LineChart
      label="p95 latency"
      labels={["12:00", "13:00", "14:00", "15:00"]}
      series={[{ name: "checkout-api", values: [180, 210, 195, 172], dashedFrom: 2 }]}
    />
  ),
  // Also appended, for the same reason: covers the hatch `<pattern>` and the
  // lighter, hatched area fill that a projected run gets under `area` —
  // the observed run's fill stays the plain opacity-20 tint.
  "LineChart/projection-area": (
    <LineChart
      label="p95 latency"
      labels={["12:00", "13:00", "14:00", "15:00"]}
      series={[{ name: "checkout-api", values: [180, 210, 195, 172], dashedFrom: 2 }]}
      area
    />
  ),
};

describe("DOM structure snapshots", () => {
  for (const [name, element] of Object.entries(cases)) {
    it(name, () => {
      const { container } = render(element);
      expect(container.innerHTML).toMatchSnapshot();
    });
  }
});
