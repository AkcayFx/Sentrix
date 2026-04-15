interface GlitchTextProps {
  text: string;
  tag?: 'h1' | 'h2' | 'h3' | 'span' | 'div';
  className?: string;
}

export default function GlitchText({ text, tag: Tag = 'h2', className = '' }: GlitchTextProps) {
  return (
    <Tag className={`glitch-text ${className}`} data-text={text}>
      {text}
    </Tag>
  );
}
