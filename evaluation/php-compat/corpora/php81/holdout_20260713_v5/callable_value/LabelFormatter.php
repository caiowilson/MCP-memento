<?php

declare(strict_types=1);

namespace RetrievalHoldout\CallableValue;

final class LabelFormatter
{
    public static function formatLabel(string $value): string
    {
        return '[' . trim($value) . ']';
    }
}
