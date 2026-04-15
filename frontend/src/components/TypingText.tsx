import { useEffect, useState } from 'react';

interface TypingTextProps {
  text: string;
  speed?: number;
  delay?: number;
  cursor?: boolean;
  tag?: 'span' | 'div' | 'h1' | 'h2' | 'h3' | 'p';
  className?: string;
  onComplete?: () => void;
}

export default function TypingText({
  text,
  speed = 50,
  delay = 0,
  cursor = true,
  tag: Tag = 'span',
  className = '',
  onComplete,
}: TypingTextProps) {
  const [displayed, setDisplayed] = useState('');
  const [started, setStarted] = useState(false);
  const [done, setDone] = useState(false);

  useEffect(() => {
    const timer = setTimeout(() => setStarted(true), delay);
    return () => clearTimeout(timer);
  }, [delay]);

  useEffect(() => {
    if (!started) return;
    if (displayed.length >= text.length) {
      setDone(true);
      onComplete?.();
      return;
    }
    const timer = setTimeout(() => {
      setDisplayed(text.slice(0, displayed.length + 1));
    }, speed + Math.random() * speed * 0.5);
    return () => clearTimeout(timer);
  }, [started, displayed, text, speed, onComplete]);

  return (
    <Tag className={`typing-text ${className}`}>
      {displayed}
      {cursor && !done && <span className="typing-cursor">_</span>}
      {cursor && done && <span className="typing-cursor blink">_</span>}
    </Tag>
  );
}
