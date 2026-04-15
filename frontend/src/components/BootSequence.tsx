import { useEffect, useState } from 'react';

const BOOT_LINES = [
  { text: '', delay: 200 },
  { text: '  ███████╗███████╗███╗   ██╗████████╗██████╗ ██╗██╗  ██╗', delay: 0 },
  { text: '  ██╔════╝██╔════╝████╗  ██║╚══██╔══╝██╔══██╗██║╚██╗██╔╝', delay: 0 },
  { text: '  ███████╗█████╗  ██╔██╗ ██║   ██║   ██████╔╝██║ ╚███╔╝ ', delay: 0 },
  { text: '  ╚════██║██╔══╝  ██║╚██╗██║   ██║   ██╔══██╗██║ ██╔██╗ ', delay: 0 },
  { text: '  ███████║███████╗██║ ╚████║   ██║   ██║  ██║██║██╔╝ ██╗', delay: 0 },
  { text: '  ╚══════╝╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝  ╚═╝╚═╝╚═╝  ╚═╝', delay: 0 },
  { text: '', delay: 300 },
  { text: '  [*] Initializing kernel modules...', delay: 80 },
  { text: '  [+] Kernel modules loaded', delay: 120 },
  { text: '  [*] Loading network interfaces...', delay: 100 },
  { text: '  [+] eth0: link up, 1000Mbps full-duplex', delay: 60 },
  { text: '  [*] Starting AI agent orchestrator...', delay: 150 },
  { text: '  [+] LLM provider connected', delay: 80 },
  { text: '  [*] Mounting sandbox containers...', delay: 100 },
  { text: '  [+] Docker runtime ready', delay: 60 },
  { text: '  [*] Initializing vector memory store...', delay: 120 },
  { text: '  [+] pgvector extension loaded', delay: 80 },
  { text: '  [*] Loading exploit database...', delay: 100 },
  { text: '  [+] 47,832 exploits indexed', delay: 60 },
  { text: '  [*] Establishing secure tunnel...', delay: 150 },
  { text: '  [+] TLS 1.3 handshake complete', delay: 80 },
  { text: '', delay: 100 },
  { text: '  ════════════════════════════════════════════', delay: 0 },
  { text: '   SENTRIX v0.4.0 — Autonomous Pentest Platform', delay: 0 },
  { text: '   Status: OPERATIONAL    Threat Level: ACTIVE', delay: 0 },
  { text: '  ════════════════════════════════════════════', delay: 0 },
  { text: '', delay: 200 },
  { text: '  root@sentrix:~# access granted_', delay: 300 },
];

interface BootSequenceProps {
  onComplete: () => void;
}

export default function BootSequence({ onComplete }: BootSequenceProps) {
  const [visibleLines, setVisibleLines] = useState(0);
  const [fadeOut, setFadeOut] = useState(false);

  useEffect(() => {
    if (visibleLines >= BOOT_LINES.length) {
      const timer = setTimeout(() => {
        setFadeOut(true);
        setTimeout(onComplete, 600);
      }, 400);
      return () => clearTimeout(timer);
    }

    const delay = BOOT_LINES[visibleLines].delay;
    const timer = setTimeout(() => {
      setVisibleLines(prev => prev + 1);
    }, delay + 30 + Math.random() * 40);

    return () => clearTimeout(timer);
  }, [visibleLines, onComplete]);

  return (
    <div className={`boot-sequence ${fadeOut ? 'boot-fade-out' : ''}`}>
      <div className="boot-screen scanlines">
        <pre className="boot-text">
          {BOOT_LINES.slice(0, visibleLines).map((line, i) => {
            const isStatus = line.text.startsWith('  [+]');
            const isAction = line.text.startsWith('  [*]');
            const isAscii = i >= 1 && i <= 6;
            const isFinal = i === BOOT_LINES.length - 1;
            const isHeader = i >= 23 && i <= 26;

            let className = '';
            if (isAscii) className = 'boot-ascii';
            else if (isStatus) className = 'boot-success';
            else if (isAction) className = 'boot-action';
            else if (isHeader) className = 'boot-header';
            else if (isFinal) className = 'boot-final';

            return (
              <span key={i} className={className}>
                {line.text}
                {'\n'}
              </span>
            );
          })}
          {visibleLines < BOOT_LINES.length && (
            <span className="boot-cursor">_</span>
          )}
        </pre>
      </div>
    </div>
  );
}
