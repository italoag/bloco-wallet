#!/bin/bash
# 
# Test script to simulate the merge develop to main workflow locally
# This helps test the merge strategy before running the GitHub Actions workflow
#

set -e

echo "🚀 Testing merge develop to main strategy locally..."

# Check if we're in a git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    echo "❌ Error: Not in a git repository"
    exit 1
fi

# Ensure we have the latest changes
echo "📥 Fetching latest changes..."
git fetch origin

# Check if develop branch exists
if ! git show-ref --verify --quiet refs/remotes/origin/develop; then
    echo "❌ Error: develop branch not found on origin"
    exit 1
fi

# Check if main branch exists  
if ! git show-ref --verify --quiet refs/remotes/origin/main; then
    echo "❌ Error: main branch not found on origin"
    exit 1
fi

# Create a test branch
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
TEST_BRANCH="test-merge-develop-to-main-${TIMESTAMP}"

echo "🌿 Creating test branch: ${TEST_BRANCH}"
git checkout -b "${TEST_BRANCH}" origin/develop

echo "🔀 Merging main with develop priority (using -X ours strategy)..."
git merge origin/main -X ours --no-ff -m "Test merge: main into develop (prioritizing develop changes)"

echo "✅ Merge simulation complete!"
echo ""
echo "📊 Summary of changes that would be made to main:"
git log --oneline origin/main..HEAD

echo ""
echo "📝 Files that would be modified:"
git diff --name-status origin/main

echo ""
echo "🧹 Cleaning up test branch..."
git checkout -
git branch -D "${TEST_BRANCH}"

echo ""
echo "✅ Test completed successfully!"
echo "💡 If this looks good, you can run the GitHub Actions workflow:"
echo "   Go to Actions → 'Merge Develop to Main' → 'Run workflow'"