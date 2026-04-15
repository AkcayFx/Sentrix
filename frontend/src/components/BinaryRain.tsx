import { useEffect, useRef } from 'react';

interface BinaryRainProps {
  opacity?: number;
  speed?: number;
  density?: number;
  className?: string;
}

export default function BinaryRain({
  opacity = 0.12,
  speed = 1,
  density = 0.6,
  className = '',
}: BinaryRainProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let animationId: number;
    let columns: number[] = [];
    let w = 0;
    let h = 0;

    const FONT_SIZE = 14;
    const CHARS = '01';

    const resize = () => {
      const dpr = window.devicePixelRatio || 1;
      const rect = canvas.getBoundingClientRect();
      w = rect.width;
      h = rect.height;
      canvas.width = w * dpr;
      canvas.height = h * dpr;
      ctx.scale(dpr, dpr);

      const colCount = Math.floor((w / FONT_SIZE) * density);
      columns = Array.from({ length: colCount }, () =>
        Math.random() * h / FONT_SIZE
      );
    };

    const draw = () => {
      ctx.fillStyle = `rgba(8, 9, 13, 0.06)`;
      ctx.fillRect(0, 0, w, h);

      ctx.font = `${FONT_SIZE}px "JetBrains Mono", monospace`;

      for (let i = 0; i < columns.length; i++) {
        const char = CHARS[Math.floor(Math.random() * CHARS.length)];
        const x = (i / columns.length) * w;
        const y = columns[i] * FONT_SIZE;

        // Head character - bright
        const brightness = 0.6 + Math.random() * 0.4;
        ctx.fillStyle = `rgba(0, 229, 195, ${brightness})`;
        ctx.fillText(char, x, y);

        // Trail characters - dimmer
        if (Math.random() > 0.85) {
          const trailChar = CHARS[Math.floor(Math.random() * CHARS.length)];
          ctx.fillStyle = `rgba(0, 229, 195, ${brightness * 0.3})`;
          ctx.fillText(trailChar, x, y - FONT_SIZE);
        }

        if (y > h && Math.random() > 0.975) {
          columns[i] = 0;
        }
        columns[i] += speed * (0.5 + Math.random() * 0.5);
      }

      animationId = requestAnimationFrame(draw);
    };

    resize();
    draw();

    window.addEventListener('resize', resize);
    return () => {
      window.removeEventListener('resize', resize);
      cancelAnimationFrame(animationId);
    };
  }, [opacity, speed, density]);

  return (
    <canvas
      ref={canvasRef}
      className={`binary-rain ${className}`}
      style={{
        position: 'absolute',
        inset: 0,
        width: '100%',
        height: '100%',
        opacity,
        pointerEvents: 'none',
        zIndex: 0,
      }}
    />
  );
}
