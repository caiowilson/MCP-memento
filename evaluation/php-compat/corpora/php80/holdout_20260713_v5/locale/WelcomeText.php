<?php

declare(strict_types=1);

namespace RetrievalHoldout\Locale;

final class WelcomeText
{
    #[HandlesLocale('pt_BR')]
    public function greet(string $name): string
    {
        return "Olá, {$name}";
    }
}
