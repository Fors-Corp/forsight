// Server Component page: library components used directly in an RSC tree
// (the package's own "use client" boundary must make this work), plus one
// element styled with the token vocabulary from the consumer's Tailwind.
import { Button, Card, CardContent, CardHeader, CardTitle, StatCard } from "@fors-corp/forsight";

export default function Page() {
  return (
    <main className="p-8">
      <Card>
        <CardHeader>
          <CardTitle>Consumer fixture</CardTitle>
        </CardHeader>
        <CardContent>
          <Button data-testid="fixture-button">Deploy</Button>
          <div data-testid="fixture-token-utility" className="bg-accent text-accent-fg rounded-md">
            token utility
          </div>
          {/* A chart-family component: measures its own width and renders SVG,
              so it also proves the package's client boundary works in an RSC tree. */}
          <StatCard
            data-testid="fixture-statcard"
            label="p95 latency"
            value="248"
            unit="ms"
            delta={-12.4}
            deltaGoodDirection="down"
            trend={[310, 288, 264, 248]}
          />
        </CardContent>
      </Card>
    </main>
  );
}
