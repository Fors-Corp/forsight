"use client";

// Every component in this entry is interactive (hooks / Radix), so the whole
// bundle is a client module — drop-in usable inside React Server Components
// without a wrapper. Server-safe theme/token utilities live in the separate
// "@marcfs31/forsight/theme" entry (see src/theme-entry.ts).

export { Button, type ButtonProps } from "./components/Button";
export { Badge, type BadgeProps } from "./components/Badge";
export { Input, type InputProps } from "./components/Input";
export { Textarea, type TextareaProps } from "./components/Textarea";
export {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
  type CardProps,
} from "./components/Card";
export { Alert, type AlertProps } from "./components/Alert";
export { Avatar, AvatarGroup, type AvatarProps, type AvatarGroupProps } from "./components/Avatar";
export { Tabs } from "./components/Tabs";
export { Heading, type HeadingProps } from "./components/Heading";
export { Text, type TextProps } from "./components/Text";
export { Checkbox, type CheckboxProps } from "./components/Checkbox";
export { RadioGroup, RadioGroupItem } from "./components/RadioGroup";
export { Switch } from "./components/Switch";
export { Toggle, type ToggleProps } from "./components/Toggle";
export { ToggleGroup, ToggleGroupItem, type ToggleGroupItemProps } from "./components/ToggleGroup";
export {
  Select,
  SelectGroup,
  SelectValue,
  SelectTrigger,
  SelectContent,
  SelectItem,
} from "./components/Select";
export { Combobox, type ComboboxProps, type ComboboxOption } from "./components/Combobox";
export {
  MultiSelect,
  type MultiSelectProps,
  type MultiSelectOption,
} from "./components/MultiSelect";
export {
  Dialog,
  DialogTrigger,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "./components/Dialog";
export {
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogAction,
  AlertDialogCancel,
} from "./components/AlertDialog";
export {
  Drawer,
  DrawerTrigger,
  DrawerClose,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerDescription,
  DrawerFooter,
} from "./components/Drawer";
export {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuGroup,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  type DropdownMenuItemProps,
} from "./components/DropdownMenu";
export { TooltipProvider, Tooltip, TooltipTrigger, TooltipContent } from "./components/Tooltip";
export {
  ToastProvider,
  ToastViewport,
  ToastRoot,
  ToastTitle,
  ToastDescription,
  ToastClose,
  Toaster,
  toast,
  dismissToast,
  useToast,
  type ToastRootProps,
} from "./components/Toast";
export { Spinner, type SpinnerProps } from "./components/Spinner";
export { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "./components/Table";
export { Progress, type ProgressProps } from "./components/Progress";
export { Popover, PopoverTrigger, PopoverAnchor, PopoverContent } from "./components/Popover";
export { HoverCard, HoverCardTrigger, HoverCardContent } from "./components/HoverCard";
export {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "./components/Accordion";
export { Slider } from "./components/Slider";
export { Skeleton } from "./components/Skeleton";
export { CopyButton, type CopyButtonProps } from "./components/CopyButton";
export { CodeBlock, type CodeBlockProps } from "./components/CodeBlock";
export { Stepper, type StepperProps, type StepperStep } from "./components/Stepper";
export {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "./components/Breadcrumb";
export {
  Pagination,
  PaginationItem,
  PaginationEllipsis,
  type PaginationItemProps,
} from "./components/Pagination";
export {
  FilterBar,
  type FilterBarProps,
  type FilterBarFacet,
  type FilterBarOption,
} from "./components/FilterBar";
export { Separator, type SeparatorProps } from "./components/Separator";
export { Label, type LabelProps } from "./components/Label";
export { Kbd, type KbdProps } from "./components/Kbd";
export { EmptyState, type EmptyStateProps } from "./components/EmptyState";
export { JSONViewer, type JSONViewerProps } from "./components/JSONViewer";
export { Collapsible, CollapsibleTrigger, CollapsibleContent } from "./components/Collapsible";
export { ScrollArea, type ScrollAreaProps } from "./components/ScrollArea";
export {
  Command,
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandSeparator,
  CommandItem,
  type CommandProps,
  type CommandDialogProps,
} from "./components/Command";
export {
  Calendar,
  DatePicker,
  DateRangePicker,
  type CalendarProps,
  type DatePickerProps,
  type DateRangePickerProps,
} from "./components/Calendar";
export {
  SidebarProvider,
  useSidebar,
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarTrigger,
  SidebarNav,
  SidebarNavItem,
  AppShell,
  AppShellMain,
  type SidebarContextValue,
  type SidebarProviderProps,
  type SidebarProps,
  type SidebarNavItemProps,
} from "./components/Sidebar";

// ── Data visualization ──────────────────────────────────────────────────────
export { ChartFrame, type ChartFrameProps, type ChartTableRow } from "./components/ChartFrame";
export { ChartLegend, type ChartLegendProps, type ChartLegendItem } from "./components/ChartLegend";
export {
  ChartTooltip,
  type ChartTooltipProps,
  type ChartTooltipRow,
} from "./components/ChartTooltip";
export { LineChart, type LineChartProps, type ChartSeries } from "./components/LineChart";
export { BarChart, type BarChartProps } from "./components/BarChart";
export { BarList, type BarListProps, type BarListItem } from "./components/BarList";
export { Funnel, type FunnelProps, type FunnelStage } from "./components/Funnel";
export { Sparkline, type SparklineProps } from "./components/Sparkline";
export { DonutChart, type DonutChartProps, type DonutSlice } from "./components/DonutChart";
export { Heatmap, type HeatmapProps, type HeatmapRow } from "./components/Heatmap";
export {
  CalendarHeatmap,
  type CalendarHeatmapProps,
  type CalendarHeatmapDay,
} from "./components/CalendarHeatmap";
export { Histogram, type HistogramProps, type HistogramBucket } from "./components/Histogram";
export { BoxPlot, type BoxPlotProps, type BoxPlotBox } from "./components/BoxPlot";
export {
  ComboChart,
  type ComboChartProps,
  type ComboChartSeries,
  type ComboChartBarSeries,
  type ComboChartLineSeries,
} from "./components/ComboChart";
export { Gauge, type GaugeProps } from "./components/Gauge";

// ── Observability ───────────────────────────────────────────────────────────
export { StatCard, type StatCardProps } from "./components/StatCard";
export { Delta, type DeltaProps, type DeltaDirection } from "./components/Delta";
export {
  StatusDot,
  STATUS_LABELS,
  type StatusDotProps,
  type ServiceStatus,
} from "./components/StatusDot";
export { UptimeBar, type UptimeBarProps, type UptimeSegment } from "./components/UptimeBar";
export {
  AlertList,
  type AlertListProps,
  type AlertListItem,
  type AlertSeverity,
} from "./components/AlertList";
export {
  ErrorBudget,
  type ErrorBudgetProps,
  type ErrorBudgetStatus,
} from "./components/ErrorBudget";
export {
  LogStream,
  LOG_LEVELS,
  type LogStreamProps,
  type LogEntry,
  type LogLevel,
} from "./components/LogStream";
export {
  Timeline,
  type TimelineProps,
  type TimelineItem,
  type TimelineTone,
} from "./components/Timeline";
export {
  TraceWaterfall,
  type TraceWaterfallProps,
  type TraceSpan,
} from "./components/TraceWaterfall";
export { TimeRange, type TimeRangeProps, type TimeRangeOption } from "./components/TimeRange";

// Chart maths, exported so an app can build a custom plot on ChartFrame that
// lands its marks on the same scales and formats as the built-in charts.
export {
  ANNOTATION_TONE_CLASSES,
  arcPath,
  areaPath,
  bandPath,
  barPath,
  clamp,
  formatCompact,
  formatDuration,
  formatPercent,
  linePath,
  niceScale,
  polar,
  project,
  seriesBg,
  seriesFill,
  seriesStroke,
  SERIES_SLOTS,
  splitAtGaps,
  splitAtProjection,
  type ChartAnnotation,
  type NiceScale,
  type Point,
} from "./lib/chart";

export { cn } from "./lib/cn";
