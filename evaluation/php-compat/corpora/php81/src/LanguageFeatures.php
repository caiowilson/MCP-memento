<?php

declare(strict_types=1);

namespace Fixture\PHP81;

interface JsonValue
{
    public function toJson(): string;
}

interface Renderable
{
    public function render(): string;
}

enum Status: string
{
    case Draft = 'draft';
    case Published = 'published';
}

final class LanguageFeatures
{
    public readonly string $id;

    public function __construct(string $id)
    {
        $this->id = $id;
    }

    public function serialize(JsonValue&Renderable $value): string
    {
        $callable = $value->render(...);

        return $value->toJson() . $callable();
    }

    public function stop(): never
    {
        throw new \RuntimeException('stopped');
    }
}
