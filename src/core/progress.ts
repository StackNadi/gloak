import { SingleBar } from "cli-progress"

export type ProgressOptions = {
  total: number
  label: string
}

type ImmediateBar = SingleBar & {
  isActive: boolean
  render(): void
  update(...args: Parameters<SingleBar["update"]>): void
}

function forceImmediateRedraw(bar: SingleBar): SingleBar {
  const immediateBar = bar as ImmediateBar
  const originalUpdate = immediateBar.update.bind(immediateBar)

  immediateBar.update = (...args: Parameters<SingleBar["update"]>) => {
    originalUpdate(...args)
    if (immediateBar.isActive) immediateBar.render()
  }

  return bar
}

export function createProgressBar(opts: ProgressOptions): SingleBar {
  const bar = forceImmediateRedraw(new SingleBar({
    format: `${opts.label} |{bar}| {percentage}% | {value}/{total} | ETA: {eta_formatted}`,
    barCompleteChar: "\u2588",
    barIncompleteChar: "\u2591",
    hideCursor: true,
    clearOnComplete: false,
    stopOnComplete: true,
    noTTYOutput: true,
    synchronousUpdate: true,
    stream: process.stderr,
  }))
  bar.start(opts.total, 0)
  return bar
}
