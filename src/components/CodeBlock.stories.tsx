import type { Meta, StoryObj } from "@storybook/react";
import { CodeBlock } from "./CodeBlock";

const meta: Meta<typeof CodeBlock> = {
  title: "Forsight/Data Display/CodeBlock",
  component: CodeBlock,
  decorators: [
    (Story) => (
      <div className="w-full max-w-xl">
        <Story />
      </div>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof CodeBlock>;

export const Command: Story = {
  render: () => <CodeBlock code="npm install @fors-corp/forsight" />,
};

export const WithLabel: Story = {
  render: () => (
    <CodeBlock
      label="curl"
      code={`curl -X POST https://api.example.com/v1/checkout \\\n  -H "Authorization: Bearer $TOKEN" \\\n  -d '{"amount": 4200}'`}
    />
  ),
};

export const WrappedLongLine: Story = {
  render: () => (
    <CodeBlock
      label="config.json"
      wrap
      code='{"service": "checkout-api", "region": "us-east-1", "replicas": 3, "resources": {"cpu": "500m", "memory": "512Mi"}, "env": "production"}'
    />
  ),
};

export const WithoutCopyButton: Story = {
  render: () => <CodeBlock code="GET /healthz -> 200 OK" showCopy={false} />,
};
