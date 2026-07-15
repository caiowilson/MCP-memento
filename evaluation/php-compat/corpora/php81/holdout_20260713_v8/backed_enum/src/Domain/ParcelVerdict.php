<?php

declare(strict_types=1);

namespace LumenParcel\Domain;

enum ParcelVerdict: string
{
    case AwaitingScan = 'awaiting_scan';
    case Cleared = 'cleared';
    case HeldForReview = 'held_for_review';
}
