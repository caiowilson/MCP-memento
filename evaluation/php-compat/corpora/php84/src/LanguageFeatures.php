<?php

declare(strict_types=1);

namespace Fixture\PHP84;

interface Titled
{
    public string $title { get; }
}

final class LanguageFeatures implements Titled
{
    public private(set) string $slug;

    public string $title {
        get => $this->title;
        set => trim($value);
    }

    public function __construct(string $slug, string $title)
    {
        $this->slug = $slug;
        $this->title = $title;
    }

    #[\Deprecated(message: 'Use title instead', since: '1.0')]
    public function legacyTitle(): string
    {
        return $this->title;
    }
}

function php84_title(): string
{
    return new LanguageFeatures('demo', 'Demo')->title;
}
