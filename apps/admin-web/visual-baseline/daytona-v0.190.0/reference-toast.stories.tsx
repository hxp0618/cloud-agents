import { Toaster } from "../sonner";
import type { StoryObj } from "@storybook/react";
import { toast } from "sonner";
import { ThemeProvider } from "../../../contexts/ThemeContext";
export default { title: "Reference/Toast", parameters: { layout: "fullscreen" } };
export const Success: StoryObj = {
  render: (_args, context) => (
    <ThemeProvider defaultTheme={context.globals.theme} storageKey="reference-toast-theme">
      <Toaster />
      <button onClick={() => toast.success("Refresh completed.")}>Show success</button>
    </ThemeProvider>
  ),
};
