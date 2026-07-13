<?php

declare(strict_types=1);

namespace Fixture\PHP80;

use Attribute;

#[Attribute(Attribute::TARGET_CLASS | Attribute::TARGET_METHOD)]
final class Route
{
    public function __construct(public string $path)
    {
    }
}

interface Logger
{
    public function write(string $message): void;
}

#[Route('/reports')]
final class LanguageFeatures
{
    public function __construct(
        public string $name,
        private Logger|string|null $logger = null,
    ) {
    }

    #[Route('/reports/status')]
    public function status(int|string $code): string
    {
        $message = match ($code) {
            200, 'ok' => 'ready',
            default => 'unknown',
        };
        $this->logger?->write(message: $message);

        $bodyLocalOnly = static function (): void {
        };
        $bodyLocalOnly();

        return $message;
    }
}
