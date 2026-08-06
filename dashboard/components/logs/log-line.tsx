"use client";

interface LogLineProps {
  timestamp?: string;
  container?: string;
  message: string;
}

const containerColors: Record<string, string> = {};
const colors = ["text-blue-400", "text-green-400", "text-yellow-400", "text-purple-400", "text-pink-400", "text-cyan-400"];
let colorIdx = 0;

function getContainerColor(container: string): string {
  if (!containerColors[container]) {
    containerColors[container] = colors[colorIdx % colors.length];
    colorIdx++;
  }
  return containerColors[container];
}

export function LogLine({ timestamp, container, message }: LogLineProps) {
  return (
    <div className="flex gap-2 font-mono text-xs leading-5">
      {timestamp && <span className="shrink-0 text-gray-500">{timestamp}</span>}
      {container && <span className={`shrink-0 ${getContainerColor(container)}`}>[{container}]</span>}
      <span className="break-all text-gray-300">{message}</span>
    </div>
  );
}
